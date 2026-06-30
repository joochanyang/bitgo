package brain

import (
	"errors"
	"testing"

	"go-bot/pkg/agent"
)

type stubLLM struct {
	out string
	err error
}

func (s stubLLM) Complete(systemPrompt, userPrompt string) (string, error) {
	return s.out, s.err
}

func TestKimiCouncilParsesDecision(t *testing.T) {
	llm := stubLLM{out: `{"action":"ENTER_LONG","size_pct":1.5,"stop_loss_pct":2,"take_profit_pct":4,"confidence":0.72,"reasoning":"breakout"}`}
	c := NewKimiCouncil(llm)
	got, err := c.Deliberate(Context{Symbol: "WLDUSDT", Regime: "trending_up", Price: 0.5})
	if err != nil {
		t.Fatalf("Deliberate: %v", err)
	}
	if got.Action != agent.ActionEnterLong || got.SizePct != 1.5 || got.Confidence != 0.72 {
		t.Fatalf("parsed decision wrong: %+v", got)
	}
}

func TestKimiCouncilFallsBackToHoldOnError(t *testing.T) {
	c := NewKimiCouncil(stubLLM{err: errors.New("suspended: insufficient balance")})
	got, err := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if err != nil {
		t.Fatalf("Deliberate should not return error on LLM failure, got %v", err)
	}
	if got.Action != agent.ActionHold {
		t.Fatalf("expected HOLD fallback, got %s", got.Action)
	}
}

func TestKimiCouncilFallsBackOnBadJSON(t *testing.T) {
	c := NewKimiCouncil(stubLLM{out: "not json at all"})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("bad JSON should fall back to HOLD, got %s", got.Action)
	}
}

func TestKimiCouncilNormalizesUnknownAction(t *testing.T) {
	c := NewKimiCouncil(stubLLM{out: `{"action":"YOLO","confidence":0.9}`})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("unknown action should normalize to HOLD, got %s", got.Action)
	}
}

func TestKimiLLMConstructs(t *testing.T) {
	var _ LLMClient = (*KimiLLM)(nil)
	k := NewKimiLLM("https://api.moonshot.ai/v1", "key", "kimi-k2.6")
	if k.baseURL == "" || k.model == "" {
		t.Fatal("KimiLLM fields not set")
	}
}
