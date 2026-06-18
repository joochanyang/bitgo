package web

import (
	"fmt"
	"time"

	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// holdStrategy is a stateless strategy that always HOLDs — used so backtests in
// web handler tests complete instantly and deterministically (0 trades).
type holdStrategy struct{}

func (holdStrategy) Name() string { return "hold" }
func (holdStrategy) Evaluate(symbol string, candles []exchange.Candle) (*strategy.Decision, error) {
	return &strategy.Decision{Decision: strategy.HOLD}, nil
}

// fakeExchange is a test double implementing exchange.Exchange. Only the kline
// methods carry behavior; the rest return zero values. Set failSymbol to make the
// kline fetch error for that symbol (used to test batch partial-failure).
type fakeExchange struct {
	candles    []exchange.Candle
	failSymbol string
}

func (f *fakeExchange) GetTicker(string) (float64, error) { return 0, nil }

func (f *fakeExchange) GetKlines(sym, _ string, limit int) ([]exchange.Candle, error) {
	if sym == f.failSymbol {
		return nil, fmt.Errorf("boom: %s", sym)
	}
	if limit > 0 && limit < len(f.candles) {
		return f.candles[:limit], nil
	}
	return f.candles, nil
}

func (f *fakeExchange) GetKlinesPaged(sym, iv string, total int) ([]exchange.Candle, error) {
	return f.GetKlines(sym, iv, total)
}

func (f *fakeExchange) GetBalance() (float64, error)                   { return 0, nil }
func (f *fakeExchange) GetPosition(string) (*exchange.Position, error) { return nil, nil }
func (f *fakeExchange) PlaceOrder(string, string, float64, float64, exchange.OrderOptions) (*exchange.OrderResult, error) {
	return nil, nil
}
func (f *fakeExchange) ClosePosition(string) error        { return nil }
func (f *fakeExchange) SetLeverage(string, int) error     { return nil }
func (f *fakeExchange) SetStopLoss(string, float64) error { return nil }

// makeFlatCandles builds n flat (price 100) hourly candles, chronological.
func makeFlatCandles(n int) []exchange.Candle {
	out := make([]exchange.Candle, n)
	base := time.Unix(1_700_000_000, 0)
	for i := range out {
		out[i] = exchange.Candle{
			Time: base.Add(time.Duration(i) * time.Hour),
			Open: 100, High: 100, Low: 100, Close: 100,
		}
	}
	return out
}
