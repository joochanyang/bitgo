// Package agent defines the shared types for the AI trading agent (Phase 1 foundation).
// AI decisions flow Council -> guard -> executor; every type here is plain data so the
// guard and memory packages can be tested without any LLM dependency.
package agent

import "time"

// Action is the kind of move the agent's Risk supervisor decided on.
type Action string

const (
	ActionEnterLong    Action = "ENTER_LONG"
	ActionEnterShort   Action = "ENTER_SHORT"
	ActionHold         Action = "HOLD"
	ActionClose        Action = "CLOSE"
	ActionPartialClose Action = "PARTIAL_CLOSE"
	ActionAdjustSL     Action = "ADJUST_SL"
)

// IsEntry reports whether the action opens a new position (so the guard must
// enforce entry-only rules like "stop-loss required" and position sizing).
func (a Action) IsEntry() bool {
	return a == ActionEnterLong || a == ActionEnterShort
}

// Decision is the structured output of the agent council (Bull/Bear/Risk).
// sizePct/stopLossPct/takeProfitPct are percentages (e.g. 1.0 == 1%).
type Decision struct {
	Action        Action  `json:"action"`
	SizePct       float64 `json:"size_pct"`
	StopLossPct   float64 `json:"stop_loss_pct"`
	TakeProfitPct float64 `json:"take_profit_pct"`
	Confidence    float64 `json:"confidence"` // 0.0 - 1.0
	Reasoning     string  `json:"reasoning"`
}

// AccountState is the live account snapshot the guard checks a Decision against.
type AccountState struct {
	Symbol            string
	Balance           float64 // quote currency (USDT)
	Price             float64
	MinOrderQty       float64 // exchange minimum order size for Symbol
	Leverage          int
	CommittedRiskUSDT float64 // summed risk of other open positions
	MaxPortfolioRisk  float64 // percent cap, e.g. 10.0
	BalanceOK         bool    // false when balance lookup failed -> block entries
}

// TradeEpisode is one full trade the agent remembers: the situation it saw, what it
// decided and why, and (after the trade closes) how it turned out. The outcome fields
// are filled in by the retrospective update when the position closes.
type TradeEpisode struct {
	ID         string    `json:"id"`
	OpenedAt   time.Time `json:"opened_at"`
	Symbol     string    `json:"symbol"`
	Regime     string    `json:"regime"` // coarse market state tag, e.g. "trending_up", "ranging"
	Decision   Decision  `json:"decision"`
	EntryPrice float64   `json:"entry_price"`
	// Retrospective (filled on close):
	Closed     bool      `json:"closed"`
	ClosedAt   time.Time `json:"closed_at,omitempty"`
	ExitPrice  float64   `json:"exit_price,omitempty"`
	PnLPct     float64   `json:"pnl_pct,omitempty"`
	ExitReason string    `json:"exit_reason,omitempty"` // "tp", "sl", "manual", "flip"
}

// Rejection records why the guard refused or downgraded part of a Decision, so the
// reason can be surfaced to the user and stored in memory.
type Rejection struct {
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// SafeDecision is a Decision that has passed the risk guard. The executor (wired in a
// later phase) will accept only SafeDecision, so a Decision that never went through
// guard.Validate cannot be executed — the type enforces what would otherwise be only a
// convention. The wrapped decision is unexported; construct via NewSafeDecision (the
// guard) and read via the accessors.
type SafeDecision struct {
	d Decision
}

// NewSafeDecision wraps a validated decision. Intended to be called only by the guard
// after Validate has applied all risk rules.
func NewSafeDecision(d Decision) SafeDecision {
	return SafeDecision{d: d}
}

// Decision returns the underlying validated decision.
func (s SafeDecision) Decision() Decision { return s.d }

// Action returns the validated action (convenience accessor).
func (s SafeDecision) Action() Action { return s.d.Action }
