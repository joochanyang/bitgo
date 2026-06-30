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

func TestLLMCouncilParsesDecision(t *testing.T) {
	llm := stubLLM{out: `{"action":"ENTER_LONG","size_pct":1.5,"stop_loss_pct":2,"take_profit_pct":4,"confidence":0.72,"reasoning":"breakout"}`}
	c := NewLLMCouncil(llm)
	got, err := c.Deliberate(Context{Symbol: "WLDUSDT", Regime: "trending_up", Price: 0.5})
	if err != nil {
		t.Fatalf("Deliberate: %v", err)
	}
	if got.Action != agent.ActionEnterLong || got.SizePct != 1.5 || got.Confidence != 0.72 {
		t.Fatalf("parsed decision wrong: %+v", got)
	}
}

func TestLLMCouncilFallsBackToHoldOnError(t *testing.T) {
	c := NewLLMCouncil(stubLLM{err: errors.New("network error")})
	got, err := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if err != nil {
		t.Fatalf("Deliberate should not return error on LLM failure, got %v", err)
	}
	if got.Action != agent.ActionHold {
		t.Fatalf("expected HOLD fallback, got %s", got.Action)
	}
}

func TestLLMCouncilFallsBackOnBadJSON(t *testing.T) {
	c := NewLLMCouncil(stubLLM{out: "not json at all"})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("bad JSON should fall back to HOLD, got %s", got.Action)
	}
}

func TestLLMCouncilNormalizesUnknownAction(t *testing.T) {
	c := NewLLMCouncil(stubLLM{out: `{"action":"YOLO","confidence":0.9}`})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("unknown action should normalize to HOLD, got %s", got.Action)
	}
}

// ```json 펜스나 앞뒤 산문으로 감싼 JSON도 파싱된다(json_object 미지원 모델 방어).
func TestLLMCouncilParsesFencedJSON(t *testing.T) {
	llm := stubLLM{out: "Here is my decision:\n```json\n{\"action\":\"ENTER_SHORT\",\"confidence\":0.6}\n```\nDone."}
	c := NewLLMCouncil(llm)
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionEnterShort {
		t.Fatalf("fenced JSON should parse to ENTER_SHORT, got %s", got.Action)
	}
}

func TestOpenAICompatLLMConstructs(t *testing.T) {
	var _ LLMClient = (*OpenAICompatLLM)(nil)
	k := NewOpenAICompatLLM("https://api.deepseek.com/v1", "key", "deepseek-v4-flash")
	if k.baseURL == "" || k.model == "" {
		t.Fatal("OpenAICompatLLM fields not set")
	}
}
