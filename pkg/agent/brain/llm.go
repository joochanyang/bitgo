package brain

import (
	"encoding/json"
	"fmt"
	"strings"

	"go-bot/pkg/agent"
)

// LLMClient is the minimal LLM call the council needs: given a system and user prompt,
// return the assistant's text (expected to be JSON). Real impl wraps pkg/ai.CallChatJSON
// against Kimi; tests inject a stub. Swapping models = swapping this implementation.
type LLMClient interface {
	Complete(systemPrompt, userPrompt string) (string, error)
}

// KimiCouncil runs the Bull/Bear/Risk deliberation as a single structured LLM call and
// maps the JSON result to a Decision. Any failure (call error, bad JSON, unknown action)
// falls back to HOLD — the council never crashes the trading loop, and the guard is the
// real safety net regardless.
type KimiCouncil struct {
	llm LLMClient
}

// NewKimiCouncil returns a Council backed by the given LLM client.
func NewKimiCouncil(llm LLMClient) *KimiCouncil {
	return &KimiCouncil{llm: llm}
}

const councilSystemPrompt = `You are a disciplined crypto futures trader. Weigh the bullish case and the bearish case, factor in the lessons from past similar trades, then output ONE final decision as strict JSON with these keys:
{"action": "ENTER_LONG|ENTER_SHORT|HOLD|CLOSE|PARTIAL_CLOSE|ADJUST_SL", "size_pct": number, "stop_loss_pct": number, "take_profit_pct": number, "confidence": number (0-1), "reasoning": string, "bull_case": string, "bear_case": string}
Be conservative: when unsure, HOLD. Always include a stop_loss_pct for entries.`

// rawDecision mirrors the LLM's JSON output before mapping to agent.Decision.
type rawDecision struct {
	Action        string  `json:"action"`
	SizePct       float64 `json:"size_pct"`
	StopLossPct   float64 `json:"stop_loss_pct"`
	TakeProfitPct float64 `json:"take_profit_pct"`
	Confidence    float64 `json:"confidence"`
	Reasoning     string  `json:"reasoning"`
}

// Deliberate builds the prompt, calls the LLM, and maps the JSON to a Decision. Falls
// back to HOLD on any failure.
func (c *KimiCouncil) Deliberate(ctx Context) (agent.Decision, error) {
	hold := agent.Decision{Action: agent.ActionHold, Confidence: 0, Reasoning: "council fallback"}

	out, err := c.llm.Complete(councilSystemPrompt, buildUserPrompt(ctx))
	if err != nil {
		return hold, nil // LLM unavailable (e.g. balance) -> safe no-op
	}

	var raw rawDecision
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return hold, nil // unparseable -> HOLD
	}

	act := agent.Action(raw.Action)
	if !knownAction(act) {
		return hold, nil // hallucinated action -> HOLD
	}
	return agent.Decision{
		Action:        act,
		SizePct:       raw.SizePct,
		StopLossPct:   raw.StopLossPct,
		TakeProfitPct: raw.TakeProfitPct,
		Confidence:    raw.Confidence,
		Reasoning:     raw.Reasoning,
	}, nil
}

func knownAction(a agent.Action) bool {
	switch a {
	case agent.ActionEnterLong, agent.ActionEnterShort, agent.ActionHold,
		agent.ActionClose, agent.ActionPartialClose, agent.ActionAdjustSL:
		return true
	}
	return false
}

// buildUserPrompt serializes the market situation and recalled past episodes.
func buildUserPrompt(ctx Context) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Symbol: %s\nRegime: %s\nPrice: %.6f\n", ctx.Symbol, ctx.Regime, ctx.Price)
	if len(ctx.Past) == 0 {
		b.WriteString("\nNo similar past trades on record.\n")
	} else {
		b.WriteString("\nLessons from similar past trades:\n")
		for _, ep := range ctx.Past {
			outcome := "open"
			if ep.Closed {
				outcome = fmt.Sprintf("closed %+.2f%% (%s)", ep.PnLPct, ep.ExitReason)
			}
			fmt.Fprintf(&b, "- %s decided %s -> %s\n", ep.Symbol, ep.Decision.Action, outcome)
		}
	}
	return b.String()
}
