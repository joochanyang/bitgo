package ai

import "testing"

// TestSanitizeDecision_RejectsUnknownAction verifies a hallucinated/garbled decision
// (the P0-5 bug: anything not in {LONG,SHORT,HOLD,CLOSE} previously fell through to an
// unintended LONG) is coerced to HOLD.
func TestSanitizeDecision_RejectsUnknownAction(t *testing.T) {
	for _, raw := range []string{"BUY", "SELL", "", "WAIT", "long", "garbage"} {
		d := &AIDecision{Decision: DecisionType(raw), Leverage: 3, StopLossPct: 2, TakeProfitPct: 4}
		sanitizeDecision(d)
		if d.Decision != HOLD {
			t.Errorf("decision %q: expected HOLD, got %q", raw, d.Decision)
		}
	}
}

// TestSanitizeDecision_KeepsValidActions verifies the four legal actions pass through unchanged.
func TestSanitizeDecision_KeepsValidActions(t *testing.T) {
	for _, valid := range []DecisionType{LONG, SHORT, HOLD, CLOSE} {
		d := &AIDecision{Decision: valid, Leverage: 3, StopLossPct: 2, TakeProfitPct: 4}
		sanitizeDecision(d)
		if d.Decision != valid {
			t.Errorf("expected %q preserved, got %q", valid, d.Decision)
		}
	}
}

// TestSanitizeDecision_ClampsStopLoss covers the P0-6 bug: a 0/negative SL would place a
// stop-loss at (or the wrong side of) the entry price.
func TestSanitizeDecision_ClampsStopLoss(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0.5},   // zero -> floor (the dangerous SL-at-entry case)
		{-3, 0.5},  // negative -> floor
		{0.1, 0.5}, // below range -> floor
		{2.5, 2.5}, // in range -> unchanged
		{99, 5.0},  // above range -> ceiling
	}
	for _, c := range cases {
		d := &AIDecision{Decision: LONG, Leverage: 3, StopLossPct: c.in, TakeProfitPct: 4}
		sanitizeDecision(d)
		if d.StopLossPct != c.want {
			t.Errorf("StopLossPct %v: expected %v, got %v", c.in, c.want, d.StopLossPct)
		}
	}
}

// TestSanitizeDecision_ClampsTakeProfit covers the take-profit half of P0-6.
func TestSanitizeDecision_ClampsTakeProfit(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0.5},   // zero -> floor
		{-1, 0.5},  // negative -> floor
		{6, 6},     // in range -> unchanged
		{50, 10.0}, // above range -> ceiling
	}
	for _, c := range cases {
		d := &AIDecision{Decision: LONG, Leverage: 3, StopLossPct: 2, TakeProfitPct: c.in}
		sanitizeDecision(d)
		if d.TakeProfitPct != c.want {
			t.Errorf("TakeProfitPct %v: expected %v, got %v", c.in, c.want, d.TakeProfitPct)
		}
	}
}

// TestSanitizeDecision_ClampsLeverage verifies the pre-existing leverage clamp still holds.
func TestSanitizeDecision_ClampsLeverage(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 1}, {-5, 1}, {3, 3}, {5, 5}, {20, 5},
	}
	for _, c := range cases {
		d := &AIDecision{Decision: LONG, Leverage: c.in, StopLossPct: 2, TakeProfitPct: 4}
		sanitizeDecision(d)
		if d.Leverage != c.want {
			t.Errorf("Leverage %d: expected %d, got %d", c.in, c.want, d.Leverage)
		}
	}
}
