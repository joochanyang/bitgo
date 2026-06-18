package exchange

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-bot/pkg/db"
)

// MockExchange implements the Exchange interface for risk-free paper trading
type MockExchange struct {
	mu        sync.RWMutex
	balance   float64
	positions map[string]*Position
	apiURL    string
}

// NewMockExchange creates a new mock exchange with a default balance
func NewMockExchange(initialBalance float64) *MockExchange {
	return &MockExchange{
		balance:   initialBalance,
		positions: make(map[string]*Position),
		apiURL:    "https://api.bybit.com", // Fetch real-time public data from Bybit
	}
}

// BybitKlineResponse represents the public kline endpoint response
type BybitKlineResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category string     `json:"category"`
		Symbol   string     `json:"symbol"`
		List     [][]string `json:"list"` // [[startTime, openPrice, highPrice, lowPrice, closePrice, volume, turnover]]
	} `json:"result"`
}

// BybitTickerResponse represents the public tickers endpoint response
type BybitTickerResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		Category string `json:"category"`
		List     []struct {
			Symbol    string `json:"symbol"`
			LastPrice string `json:"lastPrice"`
		} `json:"list"`
	} `json:"result"`
}

// GetTicker gets the latest ticker price for a symbol
func (m *MockExchange) GetTicker(symbol string) (float64, error) {
	resp, err := http.Get(fmt.Sprintf("%s/v5/market/tickers?category=linear&symbol=%s", m.apiURL, symbol))
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

	// Update unrealized PnL for active position of this symbol
	m.updatePnL(symbol, lastPrice)

	return lastPrice, nil
}

// GetKlines fetches historical candle data
func (m *MockExchange) GetKlines(symbol string, interval string, limit int) ([]Candle, error) {
	url := fmt.Sprintf("%s/v5/market/kline?category=linear&symbol=%s&interval=%s&limit=%d", m.apiURL, symbol, mapInterval(interval), limit)
	resp, err := http.Get(url)
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

	candles := parseKlineList(result.Result.List)

	if len(candles) > 0 {
		latestPrice := candles[len(candles)-1].Close
		m.updatePnL(symbol, latestPrice)
	}

	return candles, nil
}

// GetKlinesPaged returns up to `total` candles. The mock server has no paging
// support, so total is clamped to the single-request cap and delegated to GetKlines.
func (m *MockExchange) GetKlinesPaged(symbol string, interval string, total int) ([]Candle, error) {
	if total > klinePageLimit {
		total = klinePageLimit
	}
	return m.GetKlines(symbol, interval, total)
}

// GetBalance returns the simulated wallet balance
func (m *MockExchange) GetBalance() (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.balance, nil
}

// GetPosition returns the active simulated position for a symbol
func (m *MockExchange) GetPosition(symbol string) (*Position, error) {
	m.mu.Lock()
	pos, ok := m.positions[symbol]
	if ok && pos.Size > 0 {
		m.mu.Unlock()
		_, _ = m.GetTicker(symbol)
		m.mu.Lock()
		pos, ok = m.positions[symbol]
	}
	defer m.mu.Unlock()

	if !ok || pos.Size == 0 {
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

	// Make a copy to avoid concurrent modifications
	copiedPos := *pos
	return &copiedPos, nil
}

// PlaceOrder executes a simulated order
func (m *MockExchange) PlaceOrder(symbol string, side string, qty float64, price float64, opts OrderOptions) (*OrderResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get latest price if not specified
	execPrice := price
	if execPrice == 0 {
		m.mu.Unlock()
		latestPrice, err := m.GetTicker(symbol)
		m.mu.Lock()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch price for mock order: %v", err)
		}
		execPrice = latestPrice
	}

	pos, ok := m.positions[symbol]
	if !ok {
		pos = &Position{
			Symbol: symbol,
			Side:   "NONE",
			Size:   0,
		}
		m.positions[symbol] = pos
	}

	// Normalize side input: "Buy" = LONG, "Sell" = SHORT
	orderSide := "Buy"
	if strings.ToLower(side) == "sell" || strings.ToUpper(side) == "SHORT" {
		orderSide = "Sell"
	}

	// Process Paper Trading Execution Logic (One-way trading simulation)
	if opts.ReduceOnly {
		// Close or reduce existing position
		if pos.Side == "NONE" || pos.Size == 0 {
			return nil, fmt.Errorf("reduceOnly order failed: no active position")
		}

		if (orderSide == "Buy" && pos.Side != "SHORT") || (orderSide == "Sell" && pos.Side != "LONG") {
			return nil, fmt.Errorf("reduceOnly order side %s does not reduce position %s", orderSide, pos.Side)
		}

		reduceQty := qty
		if reduceQty > pos.Size {
			reduceQty = pos.Size // Cap it at position size
		}

		// Calculate realized PnL
		var pnl float64
		if pos.Side == "LONG" {
			pnl = (execPrice - pos.EntryPrice) * reduceQty
		} else {
			pnl = (pos.EntryPrice - execPrice) * reduceQty
		}

		// Deduct from balance
		m.balance += pnl
		pos.Size -= reduceQty

		if pos.Size <= 0 {
			pos.Side = "NONE"
			pos.Size = 0
			pos.EntryPrice = 0
			pos.UnrealizedPnL = 0
			pos.StopLossPrice = 0
			pos.TakeProfitPrice = 0
		} else {
			pos.UnrealizedPnL = pos.UnrealizedPnL * (pos.Size / (pos.Size + reduceQty))
			if opts.StopLossPrice > 0 {
				pos.StopLossPrice = opts.StopLossPrice
			}
			if opts.TakeProfitPrice > 0 {
				pos.TakeProfitPrice = opts.TakeProfitPrice
			}
		}
	} else {
		// Open or increase position
		cost := (qty * execPrice)
		if m.balance < cost {
			return nil, fmt.Errorf("insufficient balance for simulated trade. Required: %.2f USDT, Available: %.2f USDT", cost, m.balance)
		}

		if pos.Side == "NONE" || pos.Size == 0 {
			// New position
			if orderSide == "Buy" {
				pos.Side = "LONG"
			} else {
				pos.Side = "SHORT"
			}
			pos.Size = qty
			pos.EntryPrice = execPrice
			pos.Leverage = 3 // default mock leverage
			pos.StopLossPrice = opts.StopLossPrice
			pos.TakeProfitPrice = opts.TakeProfitPrice
		} else if (orderSide == "Buy" && pos.Side == "LONG") || (orderSide == "Sell" && pos.Side == "SHORT") {
			// Add to existing position (average entry price)
			totalSize := pos.Size + qty
			pos.EntryPrice = ((pos.EntryPrice * pos.Size) + (execPrice * qty)) / totalSize
			pos.Size = totalSize
			if opts.StopLossPrice > 0 {
				pos.StopLossPrice = opts.StopLossPrice
			}
			if opts.TakeProfitPrice > 0 {
				pos.TakeProfitPrice = opts.TakeProfitPrice
			}
		} else {
			// Opposite order in one-way mode: reduces position first, then reverses if remainder
			if qty >= pos.Size {
				// Close position and open reverse
				remainder := qty - pos.Size

				// Realize current position PnL
				var pnl float64
				if pos.Side == "LONG" {
					pnl = (execPrice - pos.EntryPrice) * pos.Size
				} else {
					pnl = (pos.EntryPrice - execPrice) * pos.Size
				}
				m.balance += pnl

				if remainder > 0 {
					if orderSide == "Buy" {
						pos.Side = "LONG"
					} else {
						pos.Side = "SHORT"
					}
					pos.Size = remainder
					pos.EntryPrice = execPrice
					pos.StopLossPrice = opts.StopLossPrice
					pos.TakeProfitPrice = opts.TakeProfitPrice
				} else {
					pos.Side = "NONE"
					pos.Size = 0
					pos.EntryPrice = 0
					pos.StopLossPrice = 0
					pos.TakeProfitPrice = 0
				}
			} else {
				// Simple reduction
				var pnl float64
				if pos.Side == "LONG" {
					pnl = (execPrice - pos.EntryPrice) * qty
				} else {
					pnl = (pos.EntryPrice - execPrice) * qty
				}
				m.balance += pnl
				pos.Size -= qty
			}
		}
	}

	pos.MarkPrice = execPrice
	// Recalculate PnL
	if pos.Side == "LONG" {
		pos.UnrealizedPnL = (pos.MarkPrice - pos.EntryPrice) * pos.Size
	} else if pos.Side == "SHORT" {
		pos.UnrealizedPnL = (pos.EntryPrice - pos.MarkPrice) * pos.Size
	} else {
		pos.UnrealizedPnL = 0
	}

	orderID := fmt.Sprintf("mock-order-%d", time.Now().UnixNano())
	return &OrderResult{
		OrderID:      orderID,
		Symbol:       symbol,
		Side:         orderSide,
		Qty:          qty,
		Price:        execPrice,
		Status:       "Filled",
		TransactTime: time.Now(),
	}, nil
}

// ClosePosition completely closes an active simulated position
func (m *MockExchange) ClosePosition(symbol string) error {
	pos, err := m.GetPosition(symbol)
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

	_, err = m.PlaceOrder(symbol, reverseSide, pos.Size, 0, OrderOptions{ReduceOnly: true})
	return err
}

// SetLeverage records the leverage for the symbol's active position (paper simulation)
func (m *MockExchange) SetLeverage(symbol string, leverage int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pos, ok := m.positions[symbol]; ok {
		pos.Leverage = leverage
	}
	return nil
}

// SetStopLoss updates the stop-loss price on an active simulated position
func (m *MockExchange) SetStopLoss(symbol string, stopLossPrice float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	pos, ok := m.positions[symbol]
	if !ok || pos.Side == "NONE" || pos.Size == 0 {
		return fmt.Errorf("no active position to set stop loss for %s", symbol)
	}
	pos.StopLossPrice = stopLossPrice
	return nil
}

// updatePnL updates the mark price and unrealized PnL of a position
func (m *MockExchange) updatePnL(symbol string, markPrice float64) {
	m.mu.Lock()

	pos, ok := m.positions[symbol]
	if !ok || pos.Size == 0 {
		m.mu.Unlock()
		return
	}

	pos.MarkPrice = markPrice
	if pos.Side == "LONG" {
		pos.UnrealizedPnL = (markPrice - pos.EntryPrice) * pos.Size
	} else if pos.Side == "SHORT" {
		pos.UnrealizedPnL = (pos.EntryPrice - markPrice) * pos.Size
	}

	// Check if Stop Loss or Take Profit triggered
	triggered := false
	var triggerType string
	if pos.Side == "LONG" {
		if pos.StopLossPrice > 0 && markPrice <= pos.StopLossPrice {
			triggered = true
			triggerType = "Stop Loss"
		} else if pos.TakeProfitPrice > 0 && markPrice >= pos.TakeProfitPrice {
			triggered = true
			triggerType = "Take Profit"
		}
	} else if pos.Side == "SHORT" {
		if pos.StopLossPrice > 0 && markPrice >= pos.StopLossPrice {
			triggered = true
			triggerType = "Stop Loss"
		} else if pos.TakeProfitPrice > 0 && markPrice <= pos.TakeProfitPrice {
			triggered = true
			triggerType = "Take Profit"
		}
	}

	if triggered {
		entry := pos.EntryPrice
		sl := pos.StopLossPrice
		tp := pos.TakeProfitPrice
		m.mu.Unlock() // Unlock to avoid deadlock in ClosePosition

		db.LogWarn("[PAPER TRADING] %s triggered for %s at %.4f (Entry: %.4f, Target: SL %.4f / TP %.4f). Auto-closing position.",
			triggerType, symbol, markPrice, entry, sl, tp)

		err := m.ClosePosition(symbol)
		if err != nil {
			db.LogError("[PAPER TRADING] Failed to close position on %s trigger: %v", triggerType, err)
		}
		return
	}

	m.mu.Unlock()
}
