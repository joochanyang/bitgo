package bot

// Trailing-stop parameters: once a position is 2% in profit, trail the stop 1.5%
// behind the current price, only ever tightening it.
const (
	trailActivationPct = 2.0
	trailOffsetPct     = 1.5
)

// trailingStopTarget computes the new trailing stop-loss for a position given the
// current price. It returns (newSL, true) only when the stop should be tightened —
// i.e. profit has reached the activation threshold AND the new stop is strictly
// tighter than the current one. This pure function is the single source of truth
// shared by RunTick (once per tick) and the real-time WS monitor (between ticks).
func trailingStopTarget(side string, entry, currentSL, currentPrice float64) (newSL float64, shouldUpdate bool) {
	switch side {
	case "LONG":
		profitPct := ((currentPrice - entry) / entry) * 100.0
		if profitPct < trailActivationPct {
			return 0, false
		}
		newSL = currentPrice * (1.0 - (trailOffsetPct / 100.0))
		if newSL > currentSL {
			return newSL, true
		}
	case "SHORT":
		profitPct := ((entry - currentPrice) / entry) * 100.0
		if profitPct < trailActivationPct {
			return 0, false
		}
		newSL = currentPrice * (1.0 + (trailOffsetPct / 100.0))
		if currentSL == 0 || newSL < currentSL {
			return newSL, true
		}
	}
	return 0, false
}
