package strategy

import (
	"testing"
	"time"

	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// makeOHLCCandles builds candles from close prices, giving each a High/Low band of
// +/- `band` around the close so ATR (which needs High/Low) is non-degenerate.
func makeOHLCCandles(closes []float64, band float64) []exchange.Candle {
	out := make([]exchange.Candle, len(closes))
	base := time.Unix(1_700_000_000, 0)
	for i, c := range closes {
		out[i] = exchange.Candle{
			Time:  base.Add(time.Duration(i) * time.Hour),
			Open:  c,
			High:  c + band,
			Low:   c - band,
			Close: c,
		}
	}
	return out
}

// rampCloses produces a varied price series (>=35 candles) so ATR is non-degenerate.
func rampCloses() []float64 {
	closes := make([]float64, 0, 50)
	price := 100.0
	for i := 0; i < 50; i++ {
		if i%3 == 0 {
			price += 2.0
		} else {
			price -= 1.0
		}
		closes = append(closes, price)
	}
	return closes
}

// TestTrendFollowingATRStopLoss verifies StopLossPct is ATR-derived (via atrStopLossPct)
// and TakeProfitPct preserves the strategy's original 3.5/1.5 R:R ratio. The SL/TP are
// written into the Decision unconditionally, so this holds regardless of the trade signal.
func TestTrendFollowingATRStopLoss(t *testing.T) {
	candles := makeOHLCCandles(rampCloses(), 2.0)
	s := NewTrendFollowing()
	dec, err := s.Evaluate("WLDUSDT", candles)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	atr, err := indicators.CalculateATR(candles, atrPeriod)
	if err != nil {
		t.Fatalf("CalculateATR: %v", err)
	}
	latest := len(candles) - 1
	wantSL := atrStopLossPct(atr[latest], candles[latest].Close)
	if !approx(dec.StopLossPct, wantSL, 1e-9) {
		t.Errorf("StopLossPct = %v, want ATR-derived %v", dec.StopLossPct, wantSL)
	}
	wantTP := wantSL * (3.5 / 1.5)
	if !approx(dec.TakeProfitPct, wantTP, 1e-9) {
		t.Errorf("TakeProfitPct = %v, want SL*ratio %v", dec.TakeProfitPct, wantTP)
	}
}
