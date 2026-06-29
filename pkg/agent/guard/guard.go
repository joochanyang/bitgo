// Package guard is the deterministic risk gate between the AI council and the
// executor. It is pure Go (no LLM): every decision that touches money passes through
// Validate, which can downgrade or reject it. The AI can never bypass this.
package guard

import "go-bot/pkg/agent"

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

	return d, rejections
}
