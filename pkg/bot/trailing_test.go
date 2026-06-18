package bot

import (
	"math"
	"testing"
)

// TestTrailingStopTarget locks the exact trailing-stop decision asymmetry that both
// RunTick and the WS monitor rely on (LONG strict tighten, SHORT currentSL==0||<).
func TestTrailingStopTarget(t *testing.T) {
	cases := []struct {
		name       string
		side       string
		entry      float64
		currentSL  float64
		price      float64
		wantUpdate bool
		wantSL     float64
	}{
		{"long below activation", "LONG", 100, 0, 101.9, false, 0},
		{"long at activation arms", "LONG", 100, 0, 102.0, true, 102.0 * 0.985},
		{"long sl already tighter", "LONG", 100, 101.0, 102.0, false, 0}, // 102*0.985=100.47 < 101
		{"long tightens", "LONG", 100, 100.0, 105.0, true, 105.0 * 0.985},
		{"short below activation", "SHORT", 100, 0, 98.1, false, 0},
		{"short at activation no sl", "SHORT", 100, 0, 98.0, true, 98.0 * 1.015},
		{"short sl already tighter", "SHORT", 100, 98.5, 98.0, false, 0}, // 98*1.015=99.47, not < 98.5
		{"short tightens", "SHORT", 100, 99.0, 95.0, true, 95.0 * 1.015},
		{"none side never updates", "NONE", 100, 0, 200.0, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotSL, gotUpdate := trailingStopTarget(tc.side, tc.entry, tc.currentSL, tc.price)
			if gotUpdate != tc.wantUpdate {
				t.Fatalf("shouldUpdate = %v, want %v", gotUpdate, tc.wantUpdate)
			}
			if gotUpdate && math.Abs(gotSL-tc.wantSL) > 1e-9 {
				t.Errorf("newSL = %v, want %v", gotSL, tc.wantSL)
			}
		})
	}
}
