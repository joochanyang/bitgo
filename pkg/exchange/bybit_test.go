package exchange

import "testing"

// TestParsePositionListSLTP verifies the position parser reads stopLoss/takeProfit
// from a Bybit V5 position-list payload so the dashboard can draw SL/TP price lines.
func TestParsePositionListSLTP(t *testing.T) {
	// A long position with both SL and TP set. Shape is the unwrapped `result`
	// object (makeRequest returns envelope.Result), i.e. {"list":[...]}.
	body := []byte(`{"list":[{
		"symbol":"WLDUSDT","side":"Buy","size":"100","entryPrice":"0.6100",
		"markPrice":"0.6200","unrealisedPnl":"1.0","leverage":"3",
		"stopLoss":"0.5900","takeProfit":"0.6500"}]}`)
	pos, err := parsePositionList("WLDUSDT", body)
	if err != nil {
		t.Fatalf("parsePositionList: %v", err)
	}
	if pos.Side != "LONG" {
		t.Errorf("Side = %q, want LONG", pos.Side)
	}
	if pos.StopLossPrice != 0.59 {
		t.Errorf("StopLossPrice = %v, want 0.59", pos.StopLossPrice)
	}
	if pos.TakeProfitPrice != 0.65 {
		t.Errorf("TakeProfitPrice = %v, want 0.65", pos.TakeProfitPrice)
	}
}

// TestParsePositionListEmptySLTP verifies that an unset SL/TP (empty string from
// Bybit) parses to 0 rather than erroring — the dashboard then draws no SL/TP line.
func TestParsePositionListEmptySLTP(t *testing.T) {
	body := []byte(`{"list":[{
		"symbol":"WLDUSDT","side":"Sell","size":"50","entryPrice":"0.70",
		"markPrice":"0.69","unrealisedPnl":"0.5","leverage":"3",
		"stopLoss":"","takeProfit":""}]}`)
	pos, err := parsePositionList("WLDUSDT", body)
	if err != nil {
		t.Fatalf("parsePositionList: %v", err)
	}
	if pos.Side != "SHORT" {
		t.Errorf("Side = %q, want SHORT", pos.Side)
	}
	if pos.StopLossPrice != 0 || pos.TakeProfitPrice != 0 {
		t.Errorf("SL/TP = %v/%v, want 0/0 when unset", pos.StopLossPrice, pos.TakeProfitPrice)
	}
}

// TestParsePositionListNoPosition verifies a flat (size 0) account returns a NONE position.
func TestParsePositionListNoPosition(t *testing.T) {
	body := []byte(`{"list":[{"symbol":"WLDUSDT","side":"","size":"0"}]}`)
	pos, err := parsePositionList("WLDUSDT", body)
	if err != nil {
		t.Fatalf("parsePositionList: %v", err)
	}
	if pos.Side != "NONE" || pos.Size != 0 {
		t.Errorf("got %s size %v, want NONE size 0", pos.Side, pos.Size)
	}
}

// TestRoundToStep covers the P1-3 quantity/price rounding: values floor to the nearest
// step so an order never rounds UP past the risk-sized amount.
func TestRoundToStep(t *testing.T) {
	cases := []struct {
		value, step, want float64
	}{
		{1.2345, 0.001, 1.234},    // qtyStep 0.001 -> floor
		{1.2399, 0.01, 1.23},      // qtyStep 0.01
		{157.89, 0.1, 157.8},      // tickSize 0.1
		{0.00067, 0.0001, 0.0006}, // small tick
		{5, 1, 5},                 // whole-number step, exact
		{5.9, 1, 5},               // whole-number step, floor
		{42.0, 0, 42.0},           // step<=0 -> unchanged
		{42.0, -1, 42.0},          // negative step -> unchanged
	}
	for _, c := range cases {
		got := roundToStep(c.value, c.step)
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("roundToStep(%v, %v) = %v, want %v", c.value, c.step, got, c.want)
		}
	}
}

// TestRoundToStepNeverRoundsUp asserts the floor invariant: the result is always <= value.
func TestRoundToStepNeverRoundsUp(t *testing.T) {
	for _, v := range []float64{0.999, 1.0001, 123.456, 0.0005} {
		for _, step := range []float64{0.001, 0.01, 0.1, 1} {
			if got := roundToStep(v, step); got > v+1e-12 {
				t.Errorf("roundToStep(%v, %v) = %v rounded UP past value", v, step, got)
			}
		}
	}
}

// TestStepDecimals verifies the decimal-place count used for formatting.
func TestStepDecimals(t *testing.T) {
	cases := []struct {
		step float64
		want int
	}{
		{1, 0},
		{0.1, 1},
		{0.01, 2},
		{0.001, 3},
		{0.0001, 4},
		{0, 8},  // unknown step -> safe default
		{-1, 8}, // invalid step -> safe default
	}
	for _, c := range cases {
		if got := stepDecimals(c.step); got != c.want {
			t.Errorf("stepDecimals(%v) = %d, want %d", c.step, got, c.want)
		}
	}
}

// TestStopLossMissingAfterEntry verifies the guard that detects when an entry
// order was placed with a stop-loss intent but the resulting position came back
// without a stop-loss actually set on the exchange (Bybit may accept the order
// yet drop the SL parameter — leaving a live position unprotected).
func TestStopLossMissingAfterEntry(t *testing.T) {
	cases := []struct {
		name         string
		intendedSL   float64 // opts.StopLossPrice requested with the order
		reduceOnly   bool
		posSize      float64 // size returned by GetPosition after the order
		posSL        float64 // stopLoss returned by GetPosition after the order
		wantNeedsSet bool
	}{
		{"sl intended but missing on position", 100.0, false, 0.5, 0, true},
		{"sl intended and present", 100.0, false, 0.5, 99.5, false},
		{"no sl intended", 0, false, 0.5, 0, false},
		{"reduce-only close needs no sl", 100.0, true, 0.5, 0, false},
		{"no position opened", 100.0, false, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stopLossNeedsRepair(c.intendedSL, c.reduceOnly, c.posSize, c.posSL)
			if got != c.wantNeedsSet {
				t.Fatalf("stopLossNeedsRepair(intendedSL=%v, reduceOnly=%v, size=%v, posSL=%v) = %v, want %v",
					c.intendedSL, c.reduceOnly, c.posSize, c.posSL, got, c.wantNeedsSet)
			}
		})
	}
}
