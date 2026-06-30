package main

import (
	"testing"
	"time"

	"go-bot/pkg/exchange"
)

// stubExchange implements exchange.Exchange with settable fields for tests. Only the
// methods buildContext/buildAccount call do anything; the rest satisfy the interface.
type stubExchange struct {
	klines     []exchange.Candle
	klinesErr  error
	ticker     float64
	tickerErr  error
	balance    float64
	balanceErr error
	positions  map[string]*exchange.Position
}

func (s *stubExchange) GetTicker(string) (float64, error) { return s.ticker, s.tickerErr }
func (s *stubExchange) GetKlines(string, string, int) ([]exchange.Candle, error) {
	return s.klines, s.klinesErr
}
func (s *stubExchange) GetKlinesPaged(string, string, int) ([]exchange.Candle, error) {
	return s.klines, s.klinesErr
}
func (s *stubExchange) GetBalance() (float64, error) { return s.balance, s.balanceErr }
func (s *stubExchange) GetPosition(sym string) (*exchange.Position, error) {
	if s.positions == nil {
		return &exchange.Position{Symbol: sym, Side: "NONE"}, nil
	}
	if p, ok := s.positions[sym]; ok {
		return p, nil
	}
	return &exchange.Position{Symbol: sym, Side: "NONE"}, nil
}
func (s *stubExchange) PlaceOrder(string, string, float64, float64, exchange.OrderOptions) (*exchange.OrderResult, error) {
	return nil, nil
}
func (s *stubExchange) ClosePosition(string) error        { return nil }
func (s *stubExchange) SetLeverage(string, int) error     { return nil }
func (s *stubExchange) SetStopLoss(string, float64) error { return nil }

// risingCandles builds a rising series so the last close sits near the channel top.
func risingCandles(n int) []exchange.Candle {
	out := make([]exchange.Candle, n)
	base := time.Unix(1700000000, 0)
	for i := 0; i < n; i++ {
		price := 0.50 + float64(i)*0.005
		out[i] = exchange.Candle{
			Time: base.Add(time.Duration(i) * time.Hour),
			Open: price, High: price + 0.002, Low: price - 0.002, Close: price, Volume: 100,
		}
	}
	return out
}

func TestBuildContextClassifiesAndPrices(t *testing.T) {
	ex := &stubExchange{klines: risingCandles(40)}
	ctx, err := buildContext(ex, "WLDUSDT", "4h", nil, 3)
	if err != nil {
		t.Fatalf("buildContext: %v", err)
	}
	if ctx.Symbol != "WLDUSDT" {
		t.Fatalf("symbol = %q", ctx.Symbol)
	}
	if ctx.Regime != "trending_up" {
		t.Fatalf("regime = %q, want trending_up (rising series)", ctx.Regime)
	}
	last := ex.klines[len(ex.klines)-1].Close
	if ctx.Price != last {
		t.Fatalf("price = %v, want last close %v", ctx.Price, last)
	}
}

func TestBuildContextErrorsOnFetchFailure(t *testing.T) {
	ex := &stubExchange{klinesErr: errFetch}
	if _, err := buildContext(ex, "WLDUSDT", "4h", nil, 3); err == nil {
		t.Fatal("expected error when kline fetch fails")
	}
}

func TestBuildContextErrorsOnTooFewCandles(t *testing.T) {
	ex := &stubExchange{klines: risingCandles(5)} // fewer than lookback
	if _, err := buildContext(ex, "WLDUSDT", "4h", nil, 3); err == nil {
		t.Fatal("expected error when not enough candles")
	}
}
