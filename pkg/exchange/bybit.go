package exchange

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-bot/pkg/db"
)

// BybitExchange implements the Exchange interface for real Bybit trading
type BybitExchange struct {
	apiKey      string
	apiSecret   string
	apiURL      string
	recvWindow  string
	client      *http.Client
	filterMu    sync.RWMutex
	filterCache map[string]instrumentFilter
}

// instrumentFilter holds the symbol's lot/price step constraints from instruments-info.
type instrumentFilter struct {
	qtyStep     float64
	minOrderQty float64
	tickSize    float64
}

// NewBybitExchange creates a new Bybit client.
// If isTestnet is true, it uses the Bybit testnet environment.
func NewBybitExchange(apiKey, apiSecret string, isTestnet bool) *BybitExchange {
	apiURL := "https://api.bybit.com"
	if isTestnet {
		apiURL = "https://api-testnet.bybit.com"
	}
	return &BybitExchange{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		apiURL:     apiURL,
		recvWindow: "5000",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		filterCache: make(map[string]instrumentFilter),
	}
}

// Common Bybit response envelope
type bybitEnvelope struct {
	RetCode int             `json:"retCode"`
	RetMsg  string          `json:"retMsg"`
	Result  json.RawMessage `json:"result"`
}

// generateSign calculates the Bybit signature
func (b *BybitExchange) generateSign(timestamp, queryStringOrBody string) string {
	val := timestamp + b.apiKey + b.recvWindow + queryStringOrBody
	h := hmac.New(sha256.New, []byte(b.apiSecret))
	h.Write([]byte(val))
	return hex.EncodeToString(h.Sum(nil))
}

// makeRequest sends an authenticated HTTP request to Bybit
func (b *BybitExchange) makeRequest(method, path string, params url.Values, body interface{}) ([]byte, error) {
	var reqBody []byte
	var err error
	var queryStr string

	if method == "POST" && body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	} else if method == "GET" && params != nil {
		queryStr = params.Encode()
	}

	fullURL := b.apiURL + path
	if queryStr != "" {
		fullURL += "?" + queryStr
	}

	var req *http.Request
	if method == "POST" {
		req, err = http.NewRequest(method, fullURL, bytes.NewBuffer(reqBody))
	} else {
		req, err = http.NewRequest(method, fullURL, nil)
	}
	if err != nil {
		return nil, err
	}

	timestamp := fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))

	// Create signature
	var signPayload string
	if method == "POST" {
		signPayload = string(reqBody)
	} else {
		signPayload = queryStr
	}
	signature := b.generateSign(timestamp, signPayload)

	// Set Headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BAPI-API-KEY", b.apiKey)
	req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
	req.Header.Set("X-BAPI-RECV-WINDOW", b.recvWindow)
	req.Header.Set("X-BAPI-SIGN", signature)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var envelope bybitEnvelope
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %v, body: %s", err, string(respBytes))
	}

	if envelope.RetCode != 0 {
		return nil, fmt.Errorf("bybit api error code %d: %s", envelope.RetCode, envelope.RetMsg)
	}

	return envelope.Result, nil
}

// GetTicker returns the latest price of a symbol
func (b *BybitExchange) GetTicker(symbol string) (float64, error) {
	// Tickers is a public endpoint
	resp, err := http.Get(fmt.Sprintf("%s/v5/market/tickers?category=linear&symbol=%s", b.apiURL, symbol))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result BybitTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if result.RetCode != 0 || len(result.Result.List) == 0 {
		return 0, fmt.Errorf("bybit api error: %s (code %d)", result.RetMsg, result.RetCode)
	}

	lastPrice, err := strconv.ParseFloat(result.Result.List[0].LastPrice, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %v", err)
	}

	return lastPrice, nil
}

// fetchKlinePage fetches a single page of candles. When endMs > 0 it requests
// candles at or before that timestamp (used to page backwards through history).
// Returns chronological (oldest-first) candles.
func (b *BybitExchange) fetchKlinePage(symbol, interval string, limit int, endMs int64) ([]Candle, error) {
	urlStr := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d", b.apiURL, symbol, mapInterval(interval), limit)
	if endMs > 0 {
		urlStr += fmt.Sprintf("&end=%d", endMs)
	}
	resp, err := http.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result BybitKlineResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.RetCode != 0 {
		return nil, fmt.Errorf("bybit api error: %s (code %d)", result.RetMsg, result.RetCode)
	}
	return parseKlineList(result.Result.List), nil
}

// GetKlines fetches historical candle data (single request, up to limit candles).
func (b *BybitExchange) GetKlines(symbol string, interval string, limit int) ([]Candle, error) {
	return b.fetchKlinePage(symbol, interval, limit, 0)
}

// GetKlinesPaged fetches up to `total` candles, paging backwards through history
// in klinePageLimit-sized requests (Bybit caps a single request at 1000).
func (b *BybitExchange) GetKlinesPaged(symbol string, interval string, total int) ([]Candle, error) {
	if total <= klinePageLimit {
		return b.GetKlines(symbol, interval, total)
	}

	var pages [][]Candle
	remaining := total
	endMs := int64(0) // 0 => start from the most recent candle
	for remaining > 0 {
		reqLimit := remaining
		if reqLimit > klinePageLimit {
			reqLimit = klinePageLimit
		}
		page, err := b.fetchKlinePage(symbol, interval, reqLimit, endMs)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break // exchange has no more history
		}
		pages = append(pages, page)
		remaining -= len(page)
		if len(page) < reqLimit {
			break // partial page => reached the oldest available candle
		}
		// Walk the window back: next page ends just before this page's oldest candle.
		endMs = page[0].Time.UnixMilli() - 1
	}
	return mergeKlinePages(pages, total), nil
}

// BybitBalanceResponse represents wallet balance result
type BybitBalanceResponse struct {
	List []struct {
		TotalEquity string `json:"totalEquity"`
		Coin        []struct {
			Coin   string `json:"coin"`
			Equity string `json:"equity"`
		} `json:"coin"`
	} `json:"list"`
}

// GetBalance returns USDT equity balance for UNIFIED account
func (b *BybitExchange) GetBalance() (float64, error) {
	params := url.Values{}
	params.Set("accountType", "UNIFIED")

	resBytes, err := b.makeRequest("GET", "/v5/account/wallet-balance", params, nil)
	if err != nil {
		// Fallback to CONTRACT account if UNIFIED fails (some older testnet/live accounts)
		params.Set("accountType", "CONTRACT")
		resBytes, err = b.makeRequest("GET", "/v5/account/wallet-balance", params, nil)
		if err != nil {
			return 0, err
		}
	}

	var balanceInfo BybitBalanceResponse
	if err := json.Unmarshal(resBytes, &balanceInfo); err != nil {
		return 0, err
	}

	if len(balanceInfo.List) == 0 {
		return 0, fmt.Errorf("no wallet balance found in list")
	}

	// Try to get totalEquity
	if eq := balanceInfo.List[0].TotalEquity; eq != "" {
		val, err := strconv.ParseFloat(eq, 64)
		if err == nil && val > 0 {
			return val, nil
		}
	}

	// Fallback to searching USDT coin equity
	for _, coin := range balanceInfo.List[0].Coin {
		if coin.Coin == "USDT" {
			equity, err := strconv.ParseFloat(coin.Equity, 64)
			if err != nil {
				return 0, err
			}
			return equity, nil
		}
	}

	return 0, fmt.Errorf("USDT balance not found")
}

// BybitPositionListResponse represents current positions result
type BybitPositionListResponse struct {
	List []struct {
		Symbol        string `json:"symbol"`
		Side          string `json:"side"` // "Buy" or "Sell"
		Size          string `json:"size"`
		EntryPrice    string `json:"entryPrice"`
		MarkPrice     string `json:"markPrice"`
		UnrealisedPnl string `json:"unrealisedPnl"`
		Leverage      string `json:"leverage"`
		StopLoss      string `json:"stopLoss"`   // "" when unset
		TakeProfit    string `json:"takeProfit"` // "" when unset
	} `json:"list"`
}

// parsePositionList maps a Bybit V5 /v5/position/list payload to a Position.
// `body` is the already-unwrapped `result` object (makeRequest returns envelope.Result),
// so it is shaped {"list":[...]}. Returns the first non-zero-size position (the bot
// holds one per symbol), or a NONE position when flat. SL/TP parse to 0 when "".
func parsePositionList(symbol string, body []byte) (*Position, error) {
	var resp BybitPositionListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	for _, item := range resp.List {
		size, _ := strconv.ParseFloat(item.Size, 64)
		if size == 0 {
			continue // No active position
		}

		entryPrice, _ := strconv.ParseFloat(item.EntryPrice, 64)
		markPrice, _ := strconv.ParseFloat(item.MarkPrice, 64)
		pnl, _ := strconv.ParseFloat(item.UnrealisedPnl, 64)
		lev, _ := strconv.Atoi(item.Leverage)
		sl, _ := strconv.ParseFloat(item.StopLoss, 64)   // "" -> 0
		tp, _ := strconv.ParseFloat(item.TakeProfit, 64) // "" -> 0

		side := "LONG"
		if item.Side == "Sell" {
			side = "SHORT"
		}

		return &Position{
			Symbol:          item.Symbol,
			Side:            side,
			Size:            size,
			EntryPrice:      entryPrice,
			MarkPrice:       markPrice,
			UnrealizedPnL:   pnl,
			Leverage:        lev,
			StopLossPrice:   sl,
			TakeProfitPrice: tp,
		}, nil
	}

	// Return empty position if none active
	return &Position{
		Symbol:        symbol,
		Side:          "NONE",
		Size:          0,
		EntryPrice:    0,
		MarkPrice:     0,
		UnrealizedPnL: 0,
		Leverage:      1,
	}, nil
}

// GetPosition returns the active position details for a symbol
func (b *BybitExchange) GetPosition(symbol string) (*Position, error) {
	params := url.Values{}
	params.Set("category", "linear")
	params.Set("symbol", symbol)

	resBytes, err := b.makeRequest("GET", "/v5/position/list", params, nil)
	if err != nil {
		return nil, err
	}

	return parsePositionList(symbol, resBytes)
}

// bybitInstrumentsInfoResponse is the subset of /v5/market/instruments-info we need.
type bybitInstrumentsInfoResponse struct {
	List []struct {
		Symbol        string `json:"symbol"`
		LotSizeFilter struct {
			QtyStep     string `json:"qtyStep"`
			MinOrderQty string `json:"minOrderQty"`
		} `json:"lotSizeFilter"`
		PriceFilter struct {
			TickSize string `json:"tickSize"`
		} `json:"priceFilter"`
	} `json:"list"`
}

// getInstrumentFilter returns the symbol's qtyStep/minOrderQty/tickSize, fetching from
// /v5/market/instruments-info on a cache miss. The endpoint is public.
func (b *BybitExchange) getInstrumentFilter(symbol string) (instrumentFilter, error) {
	b.filterMu.RLock()
	if f, ok := b.filterCache[symbol]; ok {
		b.filterMu.RUnlock()
		return f, nil
	}
	b.filterMu.RUnlock()

	resp, err := b.client.Get(fmt.Sprintf("%s/v5/market/instruments-info?category=linear&symbol=%s", b.apiURL, symbol))
	if err != nil {
		return instrumentFilter{}, err
	}
	defer resp.Body.Close()

	var env bybitEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return instrumentFilter{}, err
	}
	if env.RetCode != 0 {
		return instrumentFilter{}, fmt.Errorf("bybit instruments-info error code %d: %s", env.RetCode, env.RetMsg)
	}

	var info bybitInstrumentsInfoResponse
	if err := json.Unmarshal(env.Result, &info); err != nil {
		return instrumentFilter{}, err
	}
	if len(info.List) == 0 {
		return instrumentFilter{}, fmt.Errorf("no instrument info for %s", symbol)
	}

	item := info.List[0]
	qtyStep, _ := strconv.ParseFloat(item.LotSizeFilter.QtyStep, 64)
	minQty, _ := strconv.ParseFloat(item.LotSizeFilter.MinOrderQty, 64)
	tickSize, _ := strconv.ParseFloat(item.PriceFilter.TickSize, 64)
	f := instrumentFilter{qtyStep: qtyStep, minOrderQty: minQty, tickSize: tickSize}

	b.filterMu.Lock()
	b.filterCache[symbol] = f
	b.filterMu.Unlock()
	return f, nil
}

// roundToStep rounds value DOWN to the nearest multiple of step (floor), so an order
// quantity never rounds up past the risk-sized amount. Returns value unchanged if step<=0.
func roundToStep(value, step float64) float64 {
	if step <= 0 {
		return value
	}
	return math.Floor(value/step) * step
}

// stepDecimals returns the number of decimal places implied by a step (e.g. 0.001 -> 3),
// used to format the rounded value without floating-point noise.
func stepDecimals(step float64) int {
	if step <= 0 {
		return 8
	}
	d := 0
	for s := step; s < 1.0 && d < 12; s *= 10 {
		d++
	}
	return d
}

// BybitOrderResponse represents placed order response
type BybitOrderResponse struct {
	OrderID     string `json:"orderId"`
	OrderLinkId string `json:"orderLinkId"`
}

// PlaceOrder places a market order (standard for our bot) and optionally sets SL/TP
func (b *BybitExchange) PlaceOrder(symbol string, side string, qty float64, price float64, opts OrderOptions) (*OrderResult, error) {
	orderSide := "Buy"
	if strings.ToLower(side) == "sell" || strings.ToUpper(side) == "SHORT" {
		orderSide = "Sell"
	}

	// Look up the symbol's lot/price filters so qty and prices conform to Bybit's
	// qtyStep/tickSize. On lookup failure, fall back to legacy fixed precision so a
	// transient instruments-info error does not block trading.
	qtyDecimals, priceDecimals := 3, 4
	filterOK := false
	var filter instrumentFilter
	if f, ferr := b.getInstrumentFilter(symbol); ferr == nil {
		filter = f
		filterOK = true
		if filter.qtyStep > 0 {
			qty = roundToStep(qty, filter.qtyStep)
			qtyDecimals = stepDecimals(filter.qtyStep)
		}
		if filter.tickSize > 0 {
			priceDecimals = stepDecimals(filter.tickSize)
		}
	} else {
		db.LogWarn("Could not fetch instrument filter for %s, using default precision: %v", symbol, ferr)
	}

	// Reject an order whose rounded qty falls below the exchange minimum (would be rejected anyway,
	// but catch it here so we don't record a phantom trade). ReduceOnly close orders are exempt.
	if filterOK && !opts.ReduceOnly && filter.minOrderQty > 0 && qty < filter.minOrderQty {
		return nil, fmt.Errorf("order qty %.*f below minimum %.*f for %s", qtyDecimals, qty, qtyDecimals, filter.minOrderQty, symbol)
	}

	roundPrice := func(p float64) float64 {
		if filterOK && filter.tickSize > 0 {
			return roundToStep(p, filter.tickSize)
		}
		return p
	}

	orderType := "Market"
	var priceStr string
	if price > 0 {
		orderType = "Limit"
		priceStr = fmt.Sprintf("%.*f", priceDecimals, roundPrice(price))
	}

	payload := map[string]interface{}{
		"category":    "linear",
		"symbol":      symbol,
		"side":        orderSide,
		"orderType":   orderType,
		"qty":         fmt.Sprintf("%.*f", qtyDecimals, qty),
		"timeInForce": "GTC",
		"positionIdx": 0, // One-Way mode
	}

	if price > 0 {
		payload["price"] = priceStr
	}

	if opts.ReduceOnly {
		payload["reduceOnly"] = true
	}

	if opts.StopLossPrice > 0 {
		payload["stopLoss"] = fmt.Sprintf("%.*f", priceDecimals, roundPrice(opts.StopLossPrice))
	}
	if opts.TakeProfitPrice > 0 {
		payload["takeProfit"] = fmt.Sprintf("%.*f", priceDecimals, roundPrice(opts.TakeProfitPrice))
	}

	resBytes, err := b.makeRequest("POST", "/v5/order/create", nil, payload)
	if err != nil {
		return nil, err
	}

	var orderResp BybitOrderResponse
	if err := json.Unmarshal(resBytes, &orderResp); err != nil {
		return nil, err
	}

	// /v5/order/create returns only the orderId, never the executed price. For a market
	// order the real average fill is unknown here, so read it back from the resulting
	// position (entryPrice). Fall back to the requested limit price if the lookup fails.
	fillPrice := price
	if !opts.ReduceOnly {
		if pos, perr := b.GetPosition(symbol); perr == nil && pos.Size > 0 && pos.EntryPrice > 0 {
			fillPrice = pos.EntryPrice
		}
	}

	return &OrderResult{
		OrderID:      orderResp.OrderID,
		Symbol:       symbol,
		Side:         orderSide,
		Qty:          qty,
		Price:        fillPrice,
		Status:       "Filled",
		TransactTime: time.Now(),
	}, nil
}

// ClosePosition closes any active positions for a symbol by posting a reduce-only market order
func (b *BybitExchange) ClosePosition(symbol string) error {
	pos, err := b.GetPosition(symbol)
	if err != nil {
		return err
	}

	if pos.Side == "NONE" || pos.Size == 0 {
		return fmt.Errorf("no active position to close for %s", symbol)
	}

	reverseSide := "Sell"
	if pos.Side == "SHORT" {
		reverseSide = "Buy"
	}

	_, err = b.PlaceOrder(symbol, reverseSide, pos.Size, 0, OrderOptions{ReduceOnly: true})
	return err
}

// SetLeverage sets the leverage for a symbol (both buy and sell side, one-way mode)
func (b *BybitExchange) SetLeverage(symbol string, leverage int) error {
	levStr := strconv.Itoa(leverage)
	payload := map[string]interface{}{
		"category":     "linear",
		"symbol":       symbol,
		"buyLeverage":  levStr,
		"sellLeverage": levStr,
	}

	_, err := b.makeRequest("POST", "/v5/position/set-leverage", nil, payload)
	if err != nil {
		// retCode 110043 = "leverage not modified" (already at this value); treat as success
		if strings.Contains(err.Error(), "110043") {
			return nil
		}
		return err
	}
	return nil
}

// SetStopLoss amends the stop-loss price on an existing position via the trading-stop endpoint
func (b *BybitExchange) SetStopLoss(symbol string, stopLossPrice float64) error {
	priceDecimals := 4
	if f, ferr := b.getInstrumentFilter(symbol); ferr == nil && f.tickSize > 0 {
		stopLossPrice = roundToStep(stopLossPrice, f.tickSize)
		priceDecimals = stepDecimals(f.tickSize)
	}

	payload := map[string]interface{}{
		"category":    "linear",
		"symbol":      symbol,
		"positionIdx": 0, // One-Way mode
		"stopLoss":    fmt.Sprintf("%.*f", priceDecimals, stopLossPrice),
	}

	_, err := b.makeRequest("POST", "/v5/position/trading-stop", nil, payload)
	if err != nil {
		// retCode 34040 = "not modified" (same SL already set); treat as success
		if strings.Contains(err.Error(), "34040") {
			return nil
		}
		return err
	}
	return nil
}
