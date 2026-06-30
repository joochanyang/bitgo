// Package runner orchestrates one trading cycle for the AI agent: build context from
// market + memory, ask the council, validate with the guard, execute, and record the
// outcome. It wires Phase 1/2 components together without touching the live rule-bot.
package runner

import (
	"fmt"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
)

// classifyRegime tags the market state from where price sits in the [low, high] channel.
// Within 10% of the top -> trending_up, within 10% of the bottom -> trending_down, else
// ranging. This coarse tag is the memory recall key (matching similar past situations).
func classifyRegime(price, low, high float64) string {
	if high <= low {
		return "ranging"
	}
	span := high - low
	if price >= high-0.1*span {
		return "trending_up"
	}
	if price <= low+0.1*span {
		return "trending_down"
	}
	return "ranging"
}

// episodeID builds a collision-resistant id from symbol, an opened-at timestamp (unix
// nanos), and a short nonce. The nonce/timestamp are passed in (not generated here) so
// the function stays deterministic and testable.
func episodeID(symbol string, openedAtUnixNano int64, nonce string) string {
	return fmt.Sprintf("%s-%d-%s", symbol, openedAtUnixNano, nonce)
}

// Runner wires the agent cycle. Dependencies are injected so tests can use mocks. The
// Execute callback receives only a SafeDecision (guard-validated), and Record persists
// the episode. NowNano/Nonce make episode IDs deterministic in tests.
type Runner struct {
	Council brain.Council
	Guard   *guard.Guard
	Execute func(agent.SafeDecision) error
	Record  func(agent.TradeEpisode) error
	NowNano func() int64
	Nonce   func() string
}

// RunOnce runs one cycle: council decides, guard validates, and — only if the validated
// action is an entry — the executor runs and the episode is recorded. HOLD or a guard
// downgrade results in no execution and no record.
func (r *Runner) RunOnce(ctx brain.Context, acc agent.AccountState) error {
	decision, err := r.Council.Deliberate(ctx)
	if err != nil {
		return fmt.Errorf("council: %w", err)
	}

	safe, _ := r.Guard.Validate(decision, acc)

	if !safe.Action().IsEntry() {
		return nil // HOLD / non-entry / guard-blocked: nothing to execute
	}

	if err := r.Execute(safe); err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	ep := agent.TradeEpisode{
		ID:         episodeID(ctx.Symbol, r.NowNano(), r.Nonce()),
		Symbol:     ctx.Symbol,
		Regime:     ctx.Regime,
		Decision:   safe.Decision(),
		EntryPrice: ctx.Price,
	}
	if err := r.Record(ep); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return nil
}
