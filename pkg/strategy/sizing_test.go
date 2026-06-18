package strategy

import (
	"math"
	"testing"
)

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// TestRiskBasedQtyLossEqualsRiskPct: a stop-out must lose ~riskPct% of balance.
// balance 10000, risk 5% => risk capital 500; SL distance 50 => qty 10; loss at SL = 10*50 = 500.
func TestRiskBasedQtyLossEqualsRiskPct(t *testing.T) {
	q := RiskBasedQty(10000, 5, 50, 100, 3)
	if !approx(q, 10, 1e-9) {
		t.Fatalf("qty = %v, want 10", q)
	}
	lossAtSL := q * 50
	if !approx(lossAtSL, 500, 1e-6) {
		t.Errorf("loss at SL = %v, want 500 (5%% of 10000)", lossAtSL)
	}
}

// TestRiskBasedQtyShortSameMagnitude: SL distance is passed pre-abs, so LONG and SHORT
// of equal distance produce identical qty.
func TestRiskBasedQtyShortSameMagnitude(t *testing.T) {
	long := RiskBasedQty(10000, 5, SLDistance(100, 98), 100, 3)
	short := RiskBasedQty(10000, 5, SLDistance(100, 102), 100, 3)
	if !approx(long, short, 1e-9) {
		t.Errorf("long qty %v != short qty %v", long, short)
	}
}

// TestRiskBasedQtyZeroDistanceFallback: when stop distance is 0 (e.g. ai emits StopLossPct 0),
// fall back to legacy notional sizing so qty stays finite.
// (10000*0.05*3)/100 = 15.
func TestRiskBasedQtyZeroDistanceFallback(t *testing.T) {
	q := RiskBasedQty(10000, 5, 0, 100, 3)
	if !approx(q, 15, 1e-9) {
		t.Errorf("fallback qty = %v, want 15 (legacy notional)", q)
	}
	if math.IsInf(q, 0) || math.IsNaN(q) {
		t.Errorf("fallback qty must be finite, got %v", q)
	}
}

// TestRiskBasedQtyNotionalCap: when a tiny stop distance would size a position whose
// notional exceeds balance*leverage (more margin than the account holds), qty is capped
// so notional == balance*leverage. Floor SL 0.3% at risk 5% lev 3 would otherwise be 16.67x.
func TestRiskBasedQtyNotionalCap(t *testing.T) {
	balance, riskPct, price, lev := 10000.0, 5.0, 100.0, 3
	slDist := price * 0.3 / 100.0 // 0.3% floor => 0.3 per unit

	q := RiskBasedQty(balance, riskPct, slDist, price, lev)

	// Uncapped would be (0.05*10000)/0.3 = 1666.67 units (notional 166667 = 16.67x balance).
	// Capped: notional must equal balance*lev = 30000 => qty = 30000/100 = 300.
	wantCapped := balance * float64(lev) / price
	if !approx(q, wantCapped, 1e-9) {
		t.Fatalf("qty = %v, want capped %v (notional = balance*lev)", q, wantCapped)
	}
	notional := q * price
	if notional > balance*float64(lev)+1e-6 {
		t.Errorf("notional %v exceeds balance*lev %v", notional, balance*float64(lev))
	}
}

// TestRiskBasedQtyUncappedWhenAffordable: a normal stop distance leaves the risk-based
// size below the notional cap, so the risk invariant (loss == riskPct%) is preserved.
func TestRiskBasedQtyUncappedWhenAffordable(t *testing.T) {
	balance, riskPct, price, lev := 10000.0, 5.0, 100.0, 3
	slDist := price * 2.0 / 100.0 // 2% stop => notional 2.5x balance < 3x cap

	q := RiskBasedQty(balance, riskPct, slDist, price, lev)
	wantRiskBased := (riskPct / 100.0 * balance) / slDist
	if !approx(q, wantRiskBased, 1e-9) {
		t.Errorf("qty = %v, want risk-based %v (cap should not bind)", q, wantRiskBased)
	}
}

func TestPositionRiskUSDT(t *testing.T) {
	// A LONG of size 10 entered at 100 with SL 98 risks 10*2 = 20 USDT at stop-out.
	if got := PositionRiskUSDT(10, 100, 98); !approx(got, 20, 1e-9) {
		t.Errorf("PositionRiskUSDT = %v, want 20", got)
	}
	// SHORT (SL above entry) is the same magnitude.
	if got := PositionRiskUSDT(10, 100, 102); !approx(got, 20, 1e-9) {
		t.Errorf("PositionRiskUSDT short = %v, want 20", got)
	}
	// No stop set -> 0 (can't quantify risk; treated as none for budgeting).
	if got := PositionRiskUSDT(10, 100, 0); got != 0 {
		t.Errorf("PositionRiskUSDT no-SL = %v, want 0", got)
	}
}

func TestAvailableRiskPct(t *testing.T) {
	// balance 10000, portfolio cap 10% => 1000 USDT total risk budget.
	cases := []struct {
		name         string
		committedUSD float64
		requestedPct float64
		wantPct      float64
	}{
		{"nothing committed -> full requested", 0, 5, 5},
		{"half budget used -> remaining caps it", 500, 5, 5},  // remaining 500 = 5% of 10000, exactly fits
		{"most budget used -> reduced", 800, 5, 2},            // remaining 200 = 2% of 10000
		{"budget exhausted -> zero", 1000, 5, 0},              // remaining 0
		{"over budget -> zero, never negative", 1500, 5, 0},   // remaining negative -> clamped 0
		{"requested below remaining -> unchanged", 200, 3, 3}, // remaining 800=8%, but requested only 3
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AvailableRiskPct(10000, 10, tc.committedUSD, tc.requestedPct)
			if !approx(got, tc.wantPct, 1e-9) {
				t.Errorf("AvailableRiskPct = %v, want %v", got, tc.wantPct)
			}
		})
	}
}

func TestSLDistanceAbs(t *testing.T) {
	if d := SLDistance(100, 102); !approx(d, 2, 1e-9) {
		t.Errorf("SLDistance(100,102) = %v, want 2", d)
	}
	if d := SLDistance(100, 98); !approx(d, 2, 1e-9) {
		t.Errorf("SLDistance(100,98) = %v, want 2", d)
	}
}

func TestAtrStopLossPctConversion(t *testing.T) {
	// k=1.5, atr=2, price=100 => (1.5*2/100)*100 = 3.0
	if pct := atrStopLossPct(2, 100); !approx(pct, 3.0, 1e-9) {
		t.Errorf("atrStopLossPct(2,100) = %v, want 3.0", pct)
	}
}

func TestAtrStopLossPctFloorOnFlat(t *testing.T) {
	// ATR 0 (flat candles) => clamps to the floor, never 0 (prevents qty explosion).
	if pct := atrStopLossPct(0, 100); !approx(pct, minStopLossPct, 1e-9) {
		t.Errorf("atrStopLossPct(0,100) = %v, want floor %v", pct, minStopLossPct)
	}
}

func TestAtrStopLossPctCeiling(t *testing.T) {
	// Huge ATR clamps to the ceiling.
	if pct := atrStopLossPct(1000, 100); !approx(pct, maxStopLossPct, 1e-9) {
		t.Errorf("atrStopLossPct(1000,100) = %v, want ceiling %v", pct, maxStopLossPct)
	}
}
