// Package guard is the deterministic risk gate between the AI council and the
// executor. It is pure Go (no LLM): every decision that touches money passes through
// Validate, which can downgrade or reject it. The AI can never bypass this.
package guard

import (
	"go-bot/pkg/agent"
	"go-bot/pkg/strategy"
)

// Guard validates AI decisions against deterministic risk rules.
type Guard struct {
	minConfidence float64
}

// New returns a Guard. minConfidence is the threshold below which an entry decision
// is downgraded to HOLD (the council was too unsure to risk money).
func New(minConfidence float64) *Guard {
	return &Guard{minConfidence: minConfidence}
}

// Validate applies risk rules to a decision and returns the safe-to-execute version
// plus any rejections (rule violations that were corrected). Rules are additive.
func (g *Guard) Validate(d agent.Decision, acc agent.AccountState) (agent.Decision, []agent.Rejection) {
	var rejections []agent.Rejection

	// Rule: low-confidence entries are downgraded to HOLD.
	if d.Action.IsEntry() && d.Confidence < g.minConfidence {
		rejections = append(rejections, agent.Rejection{
			Rule:    "min_confidence",
			Message: "entry confidence below threshold; downgraded to HOLD",
		})
		d.Action = agent.ActionHold
	}

	// Rule: entries must declare a stop-loss. A leveraged position with no SL can lose
	// far more than intended — block it. (Mirrors the live rule-bot's SL guarantee.)
	if d.Action.IsEntry() && d.StopLossPct <= 0 {
		rejections = append(rejections, agent.Rejection{
			Rule:    "stop_loss_required",
			Message: "entry has no stop-loss; downgraded to HOLD",
		})
		d.Action = agent.ActionHold
	}

	// Rule: never open a position when the balance lookup failed — sizing would be
	// guesswork. (The live bot hit balance:0 once; this blocks that path.)
	if d.Action.IsEntry() && !acc.BalanceOK {
		rejections = append(rejections, agent.Rejection{
			Rule:    "balance_unknown",
			Message: "balance unavailable; entry blocked",
		})
		d.Action = agent.ActionHold
	}

	// Rule: clamp entry size to the remaining portfolio risk budget. Reuses the live
	// bot's AvailableRiskPct so the AI agent obeys the same 10% portfolio cap. If the
	// budget is exhausted (0 available), the entry is downgraded to HOLD.
	if d.Action.IsEntry() {
		avail := strategy.AvailableRiskPct(acc.Balance, acc.MaxPortfolioRisk, acc.CommittedRiskUSDT, d.SizePct)
		// epsilon guards against float residue at an exactly-exhausted budget
		// (balance*0.1 - committed can leave ~1e-15 instead of 0).
		const minSizePct = 1e-9
		if avail <= minSizePct {
			rejections = append(rejections, agent.Rejection{
				Rule:    "portfolio_risk_clamp",
				Message: "portfolio risk budget exhausted; entry blocked",
			})
			d.Action = agent.ActionHold
		} else if avail < d.SizePct {
			rejections = append(rejections, agent.Rejection{
				Rule:    "portfolio_risk_clamp",
				Message: "size reduced to fit remaining portfolio risk budget",
			})
			d.SizePct = avail
		}
	}

	// Rule: block entries whose risk-based qty falls below the exchange minimum. Forcing
	// the minimum would size a position larger than the intended risk% (a real hazard on
	// a small account with a high-priced coin — e.g. BTC at 50 USDT). HOLD instead.
	if d.Action.IsEntry() && acc.MinOrderQty > 0 && acc.Price > 0 {
		slDistPerUnit := acc.Price * (d.StopLossPct / 100.0)
		qty := strategy.RiskBasedQty(acc.Balance, d.SizePct, slDistPerUnit, acc.Price, acc.Leverage)
		if qty < acc.MinOrderQty {
			rejections = append(rejections, agent.Rejection{
				Rule:    "below_min_order_qty",
				Message: "risk-based qty below exchange minimum; entry blocked to avoid oversizing",
			})
			d.Action = agent.ActionHold
		}
	}

	return d, rejections
}
