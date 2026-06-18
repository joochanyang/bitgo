package strategy

import (
	"testing"

	"go-bot/pkg/indicators"
)

// TestMeanReversionATRStopLoss verifies ATR-derived StopLossPct and TakeProfitPct
// preserving the strategy's original 2.5/1.25 (=2.0R) ratio. SL/TP are written into
// the Decision unconditionally, so this holds regardless of the trade signal.
func TestMeanReversionATRStopLoss(t *testing.T) {
	candles := makeOHLCCandles(rampCloses(), 1.5)
	s := NewMeanReversion()
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
	wantTP := wantSL * (2.5 / 1.25)
	if !approx(dec.TakeProfitPct, wantTP, 1e-9) {
		t.Errorf("TakeProfitPct = %v, want SL*ratio %v", dec.TakeProfitPct, wantTP)
	}
}
