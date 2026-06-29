// Package brain is the AI decision layer abstraction. The Council interface is what the
// runner depends on; Phase 2 will add a real Bull/Bear/Risk implementation backed by an
// LLM. Phase 1 ships only MockCouncil so the pipeline (trigger -> council -> guard) can
// be wired and tested with no API cost or model dependency. Keeping the LLM behind an
// interface means swapping Claude/Gemini/GPT later is a one-line change.
package brain

import "go-bot/pkg/agent"

// Context is the situation snapshot handed to the council: the live market view plus
// the recalled past episodes the agent should reason over. Phase 2 fills in indicator
// fields; Phase 1 keeps the minimal shape the runner needs.
type Context struct {
	Symbol string
	Regime string
	Price  float64
	Past   []agent.TradeEpisode // recalled by memory
}

// Council turns a Context into a structured Decision. Real implementations run the
// Bull/Bear/Risk deliberation; mocks return a canned decision.
type Council interface {
	Deliberate(ctx Context) (agent.Decision, error)
}

// MockCouncil returns a fixed decision regardless of input. For tests and dry wiring.
type MockCouncil struct {
	fixed agent.Decision
}

// NewMockCouncil returns a Council that always decides `fixed`.
func NewMockCouncil(fixed agent.Decision) *MockCouncil {
	return &MockCouncil{fixed: fixed}
}

// Deliberate returns the fixed decision.
func (m *MockCouncil) Deliberate(ctx Context) (agent.Decision, error) {
	return m.fixed, nil
}
