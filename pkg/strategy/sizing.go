package strategy

import "math"

// Hardcoded ATR sizing/stop-loss defaults (not exposed in config).
const (
	atrPeriod      = 14   // ATR lookback used by the rule strategies
	atrK           = 1.5  // stop-loss distance = atrK * ATR
	minStopLossPct = 0.3  // floor: keeps qty bounded when ATR ~0 (flat candles)
	maxStopLossPct = 10.0 // ceiling: avoids an ATR stop eating the whole risk budget over too wide a band
)

// atrStopLossPct converts an ATR value into a stop-loss percent of price using
// the default atrK multiplier, clamped to [minStopLossPct, maxStopLossPct].
func atrStopLossPct(atr, price float64) float64 {
	return atrStopLossPctK(atr, price, atrK)
}

// atrStopLossPctK is atrStopLossPct with a caller-supplied ATR multiplier k, so a
// strategy can tune its stop width without affecting the others. A zero ATR (flat
// candles) clamps to the floor so risk-based sizing can never divide by zero.
func atrStopLossPctK(atr, price, k float64) float64 {
	if price <= 0 {
		return maxStopLossPct
	}
	pct := (k * atr / price) * 100.0
	if pct < minStopLossPct {
		return minStopLossPct
	}
	if pct > maxStopLossPct {
		return maxStopLossPct
	}
	return pct
}

// SLDistance is the absolute per-unit distance between entry and stop-loss price.
func SLDistance(entry, slPrice float64) float64 { return math.Abs(entry - slPrice) }

// PositionRiskUSDT is the loss (in quote currency) an open position would realize if
// it hit its stop: size * |entry - slPrice|. A position with no stop (slPrice 0) is
// treated as 0 risk for budgeting (its risk can't be quantified here).
func PositionRiskUSDT(size, entry, slPrice float64) float64 {
	if slPrice <= 0 {
		return 0
	}
	return size * math.Abs(entry-slPrice)
}

// AvailableRiskPct returns the risk percent a NEW entry may use so that total
// committed risk across the portfolio stays within maxPortfolioRiskPct of balance.
// committedRiskUSDT is the summed PositionRiskUSDT of currently-open positions.
// The result is clamped to [0, requestedPct]: it never exceeds what the strategy
// asked for, and is 0 when the portfolio budget is already exhausted.
func AvailableRiskPct(balance, maxPortfolioRiskPct, committedRiskUSDT, requestedPct float64) float64 {
	if balance <= 0 {
		return 0
	}
	budgetUSDT := balance * (maxPortfolioRiskPct / 100.0)
	remainingUSDT := budgetUSDT - committedRiskUSDT
	if remainingUSDT <= 0 {
		return 0
	}
	remainingPct := remainingUSDT / balance * 100.0
	if remainingPct < requestedPct {
		return remainingPct
	}
	return requestedPct
}

// RiskBasedQty sizes a position so that a stop-out loses ~riskPct% of balance,
// independent of leverage: qty = (riskPct/100 * balance) / slDistancePerUnit.
//
// The result is capped so the position's notional (qty*price) never exceeds
// balance*leverage — i.e. required margin never exceeds the account balance.
// Without this cap, a very tight stop (small slDistancePerUnit, e.g. the ATR floor
// in a low-volatility regime) would size an unfundable position that the exchange
// rejects or that fills at unintended over-leverage. When the cap binds, the
// realized loss at the stop is LESS than riskPct% (the safe direction).
//
// If slDistancePerUnit is non-positive (e.g. ai_strategy emits StopLossPct 0),
// it falls back to legacy notional sizing (balance * riskPct% * leverage / price)
// so qty stays finite rather than exploding to infinity.
func RiskBasedQty(balance, riskPct, slDistancePerUnit, price float64, leverage int) float64 {
	riskCapital := balance * (riskPct / 100.0)

	var qty float64
	if slDistancePerUnit > 0 {
		qty = riskCapital / slDistancePerUnit
	} else if price > 0 {
		qty = (riskCapital * float64(leverage)) / price
	} else {
		return 0
	}

	// Cap notional at balance*leverage so margin never exceeds the account.
	if price > 0 && leverage > 0 {
		maxQty := (balance * float64(leverage)) / price
		if qty > maxQty {
			qty = maxQty
		}
	}
	return qty
}
