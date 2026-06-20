package strategy

import (
	"testing"
	"time"

	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// makeBreakoutCandles builds candles from explicit OHLCV rows so breakout/volume
// behavior can be asserted precisely (the shared makeOHLCCandles only varies Close).
type ohlcv struct {
	open, high, low, clse, vol float64
}

func makeBreakoutCandles(rows []ohlcv) []exchange.Candle {
	out := make([]exchange.Candle, len(rows))
	base := time.Unix(1_700_000_000, 0)
	for i, r := range rows {
		out[i] = exchange.Candle{
			Time:   base.Add(time.Duration(i) * time.Hour),
			Open:   r.open,
			High:   r.high,
			Low:    r.low,
			Close:  r.clse,
			Volume: r.vol,
		}
	}
	return out
}

func TestRollingHighExcludesCurrentBar(t *testing.T) {
	// Prior window highs: 10,11,12. Current bar (idx 3) has a high of 99 which
	// must be EXCLUDED — the breakout level is set by bars BEFORE the current one.
	rows := []ohlcv{
		{high: 10, low: 8},
		{high: 11, low: 9},
		{high: 12, low: 10},
		{high: 99, low: 50}, // current bar — excluded from its own level
	}
	c := makeBreakoutCandles(rows)
	got := rollingHigh(c, 3, 3)
	if got != 12 {
		t.Errorf("rollingHigh = %v, want 12 (max of prior 3 bars, excluding current)", got)
	}
}

func TestRollingLowExcludesCurrentBar(t *testing.T) {
	rows := []ohlcv{
		{high: 10, low: 8},
		{high: 11, low: 7},
		{high: 12, low: 9},
		{high: 99, low: 1}, // current bar — excluded
	}
	c := makeBreakoutCandles(rows)
	got := rollingLow(c, 3, 3)
	if got != 7 {
		t.Errorf("rollingLow = %v, want 7 (min of prior 3 bars, excluding current)", got)
	}
}

func TestAvgVolumeExcludesCurrentBar(t *testing.T) {
	rows := []ohlcv{
		{vol: 100},
		{vol: 200},
		{vol: 300},
		{vol: 9999}, // current bar — excluded
	}
	c := makeBreakoutCandles(rows)
	got := avgVolume(c, 3, 3)
	if !approx(got, 200, 1e-9) {
		t.Errorf("avgVolume = %v, want 200 (mean of prior 3 bars)", got)
	}
}

// flatThenBreakout returns >=35 candles: a noisy-but-rangebound base, then a final
// bar whose close pierces above the prior rolling high on strong volume.
func flatThenBreakout(breakUp bool) []exchange.Candle {
	rows := make([]ohlcv, 0, 40)
	// Rangebound base oscillating 99..101 with steady volume.
	for i := 0; i < 39; i++ {
		hi, lo := 101.0, 99.0
		cl := 100.0
		if i%2 == 0 {
			cl = 100.5
		}
		rows = append(rows, ohlcv{open: cl, high: hi, low: lo, clse: cl, vol: 1000})
	}
	if breakUp {
		// Close 105 > prior rolling high 101, volume well above average.
		rows = append(rows, ohlcv{open: 101, high: 106, low: 101, clse: 105, vol: 5000})
	} else {
		// Close 95 < prior rolling low 99, volume well above average.
		rows = append(rows, ohlcv{open: 99, high: 99, low: 94, clse: 95, vol: 5000})
	}
	return makeBreakoutCandles(rows)
}

func TestVolatilityBreakoutLongOnUpsideBreak(t *testing.T) {
	s := NewVolatilityBreakout()
	dec, err := s.Evaluate("WLDUSDT", flatThenBreakout(true))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Decision != LONG {
		t.Errorf("Decision = %v, want LONG on upside breakout. Reasoning: %s", dec.Decision, dec.Reasoning)
	}
}

func TestVolatilityBreakoutShortOnDownsideBreak(t *testing.T) {
	s := NewVolatilityBreakout()
	dec, err := s.Evaluate("WLDUSDT", flatThenBreakout(false))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Decision != SHORT {
		t.Errorf("Decision = %v, want SHORT on downside breakdown. Reasoning: %s", dec.Decision, dec.Reasoning)
	}
}

func TestVolatilityBreakoutHoldInsideRange(t *testing.T) {
	// Rangebound series whose final close stays inside the prior high/low band.
	rows := make([]ohlcv, 0, 40)
	for i := 0; i < 40; i++ {
		cl := 100.0
		if i%2 == 0 {
			cl = 100.5
		}
		rows = append(rows, ohlcv{open: cl, high: 101, low: 99, clse: cl, vol: 1000})
	}
	s := NewVolatilityBreakout()
	dec, err := s.Evaluate("WLDUSDT", makeBreakoutCandles(rows))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Decision != HOLD {
		t.Errorf("Decision = %v, want HOLD when price stays inside the range", dec.Decision)
	}
}

func TestVolatilityBreakoutVolumeFilterBlocks(t *testing.T) {
	// Same upside price breakout, but volume on the breakout bar is BELOW average —
	// a breakout without participation should be filtered to HOLD.
	rows := make([]ohlcv, 0, 40)
	for i := 0; i < 39; i++ {
		cl := 100.0
		if i%2 == 0 {
			cl = 100.5
		}
		rows = append(rows, ohlcv{open: cl, high: 101, low: 99, clse: cl, vol: 1000})
	}
	// Price pierces 101 but volume (100) is well below the ~1000 average.
	rows = append(rows, ohlcv{open: 101, high: 106, low: 101, clse: 105, vol: 100})
	s := NewVolatilityBreakout()
	dec, err := s.Evaluate("WLDUSDT", makeBreakoutCandles(rows))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Decision != HOLD {
		t.Errorf("Decision = %v, want HOLD when breakout lacks volume confirmation. Reasoning: %s", dec.Decision, dec.Reasoning)
	}
}

// TestVolatilityBreakoutATRStopLoss verifies StopLossPct is ATR-derived and
// TakeProfitPct preserves the strategy's 2.0R ratio (SL/TP written unconditionally).
func TestVolatilityBreakoutATRStopLoss(t *testing.T) {
	candles := makeOHLCCandles(rampCloses(), 2.0)
	s := NewVolatilityBreakout()
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
	wantTP := wantSL * breakoutRewardRisk
	if !approx(dec.TakeProfitPct, wantTP, 1e-9) {
		t.Errorf("TakeProfitPct = %v, want SL*%.1f = %v", dec.TakeProfitPct, breakoutRewardRisk, wantTP)
	}
}

func TestVolatilityBreakoutInsufficientData(t *testing.T) {
	rows := make([]ohlcv, 10) // fewer than the required minimum
	for i := range rows {
		rows[i] = ohlcv{open: 100, high: 101, low: 99, clse: 100, vol: 1000}
	}
	s := NewVolatilityBreakout()
	dec, err := s.Evaluate("WLDUSDT", makeBreakoutCandles(rows))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if dec.Decision != HOLD {
		t.Errorf("Decision = %v, want HOLD on insufficient data", dec.Decision)
	}
}

func TestVolatilityBreakoutName(t *testing.T) {
	if got := NewVolatilityBreakout().Name(); got != "volatility_breakout" {
		t.Errorf("Name = %q, want volatility_breakout", got)
	}
}

// TestVolatilityBreakoutWithParamsDefaultsMatch asserts the parameterized
// constructor with the default values produces the same fields as the plain
// constructor — i.e. live/backtest behavior is unchanged.
func TestVolatilityBreakoutWithParamsDefaultsMatch(t *testing.T) {
	def := NewVolatilityBreakout()
	if def.lookback != breakoutLookback || def.rewardRisk != breakoutRewardRisk || def.atrK != atrK {
		t.Fatalf("default ctor = {lb:%d rr:%.2f k:%.2f}, want {%d %.2f %.2f}",
			def.lookback, def.rewardRisk, def.atrK, breakoutLookback, breakoutRewardRisk, atrK)
	}
	p := NewVolatilityBreakoutWithParams(breakoutLookback, breakoutRewardRisk, atrK)
	if *p != *def {
		t.Errorf("WithParams(defaults) = %+v, want %+v", *p, *def)
	}
}

// TestVolatilityBreakoutWithParamsNonPositiveFallback verifies a degenerate combo
// falls back to defaults rather than producing a zero-window strategy.
func TestVolatilityBreakoutWithParamsNonPositiveFallback(t *testing.T) {
	p := NewVolatilityBreakoutWithParams(0, 0, 0)
	if p.lookback != breakoutLookback || p.rewardRisk != breakoutRewardRisk || p.atrK != atrK {
		t.Errorf("non-positive params = %+v, want defaults", *p)
	}
}

// TestVolatilityBreakoutRewardRiskAffectsTP confirms the reward:risk parameter
// actually changes the take-profit (TP = SL * rewardRisk) while SL is unchanged.
func TestVolatilityBreakoutRewardRiskAffectsTP(t *testing.T) {
	candles := makeOHLCCandles(rampCloses(), 2.0)
	base := NewVolatilityBreakout()
	wide := NewVolatilityBreakoutWithParams(breakoutLookback, 3.0, atrK)

	bd, err := base.Evaluate("WLDUSDT", candles)
	if err != nil {
		t.Fatalf("base Evaluate: %v", err)
	}
	wd, err := wide.Evaluate("WLDUSDT", candles)
	if err != nil {
		t.Fatalf("wide Evaluate: %v", err)
	}
	if !approx(bd.StopLossPct, wd.StopLossPct, 1e-9) {
		t.Errorf("SL changed across reward:risk: %v vs %v (should be equal)", bd.StopLossPct, wd.StopLossPct)
	}
	if !approx(wd.TakeProfitPct, wd.StopLossPct*3.0, 1e-9) {
		t.Errorf("TP = %v, want SL*3.0 = %v", wd.TakeProfitPct, wd.StopLossPct*3.0)
	}
	if approx(bd.TakeProfitPct, wd.TakeProfitPct, 1e-9) {
		t.Errorf("TP did not change with reward:risk (%v vs %v)", bd.TakeProfitPct, wd.TakeProfitPct)
	}
}

// TestVolatilityBreakoutAtrKAffectsSL confirms a larger ATR multiplier widens the
// stop-loss percent (when not clamped).
func TestVolatilityBreakoutAtrKAffectsSL(t *testing.T) {
	candles := makeOHLCCandles(rampCloses(), 2.0)
	narrow := NewVolatilityBreakoutWithParams(breakoutLookback, breakoutRewardRisk, 1.0)
	wide := NewVolatilityBreakoutWithParams(breakoutLookback, breakoutRewardRisk, 2.5)

	nd, err := narrow.Evaluate("WLDUSDT", candles)
	if err != nil {
		t.Fatalf("narrow Evaluate: %v", err)
	}
	wd, err := wide.Evaluate("WLDUSDT", candles)
	if err != nil {
		t.Fatalf("wide Evaluate: %v", err)
	}
	if wd.StopLossPct <= nd.StopLossPct {
		t.Errorf("larger atrK should widen SL: narrow=%v wide=%v", nd.StopLossPct, wd.StopLossPct)
	}
}
