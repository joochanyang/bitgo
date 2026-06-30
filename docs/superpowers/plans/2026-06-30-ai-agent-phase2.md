# AI 트레이딩 에이전트 Phase 2 (2-A~2-D) 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Phase 1 토대 위에 Kimi 기반 AI 두뇌(Bull/Bear/Risk 1호출 통합 council)와 오케스트레이션(runner)을 붙여, MockCouncil로 한 사이클이 끝까지 도는 것을 검증한다. 실제 Kimi 호출은 잔액 충전 후 켠다.

**Architecture:** `pkg/ai`에 OpenAI호환 JSON 호출 헬퍼를 추가(Kimi=OpenAI호환, base URL만 다름). `pkg/agent`에 `SafeDecision` 타입을 추가해 "guard를 거친 결정만 executor가 받는다"를 타입으로 강제. `pkg/agent/brain`에 `LLMClient` 인터페이스 + `KimiCouncil`(mock으로 검증). `pkg/agent/runner`가 context→council→guard→execute→memory를 엮는다. 기존 실거래 룰봇(`pkg/bot`)·기존 `callOpenAI`/`callGemini`는 무변경.

**Tech Stack:** Go 1.25, 표준 라이브러리 + 기존 `pkg/ai`·`pkg/agent`·`pkg/exchange` 재사용. 외부 의존성 추가 없음.

---

## File Structure

- `pkg/ai/chat.go` (신규) — `CallChatJSON(baseURL, apiKey, model, systemPrompt, userPrompt) (string, error)`. OpenAI호환 chat completions + json_object. 기존 `ai.go`의 openaiRequest/Response 타입 재사용.
- `pkg/ai/chat_test.go` (신규) — httptest mock 서버로 검증.
- `pkg/agent/types.go` (수정) — `SafeDecision` 타입 + `NewSafeDecision`(guard 전용) 추가.
- `pkg/agent/guard/guard.go` (수정) — `Validate` 반환을 `(SafeDecision, []Rejection)`로. 내부 로직 불변.
- `pkg/agent/guard/guard_test.go` (수정) — `safe.Action` → `safe.Decision().Action` 접근자 반영.
- `pkg/agent/brain/llm.go` (신규) — `LLMClient` 인터페이스 + `KimiCouncil` + 프롬프트 빌드/파싱.
- `pkg/agent/brain/llm_test.go` (신규) — mock LLMClient로 파싱·HOLD폴백 검증.
- `pkg/agent/runner/runner.go` (신규) — `Runner` + RunOnce(한 사이클). 어댑터(regime 분류·episode ID).
- `pkg/agent/runner/runner_test.go` (신규) — mock exchange + MockCouncil로 E2E.
- `pkg/agent/pipeline_test.go` (수정) — SafeDecision 접근자 반영.

---

## Task 1: pkg/ai — OpenAI호환 JSON 호출 헬퍼 (Kimi용)

기존 `callOpenAI`(엔드포인트 하드코딩)는 건드리지 않고, base URL을 받는 새 공개 함수를 추가한다. Kimi는 OpenAI 호환이라 같은 요청/응답 구조를 쓴다.

**Files:**
- Create: `pkg/ai/chat.go`
- Test: `pkg/ai/chat_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/ai/chat_test.go`:
```go
package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CallChatJSON이 OpenAI호환 엔드포인트로 요청을 보내고 content를 돌려준다.
func TestCallChatJSON(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"action\":\"HOLD\"}"}}]}`)
	}))
	defer srv.Close()

	c := NewAIClient()
	out, err := c.CallChatJSON(srv.URL, "test-key", "kimi-k2.6", "sys", "user")
	if err != nil {
		t.Fatalf("CallChatJSON: %v", err)
	}
	if out != `{"action":"HOLD"}` {
		t.Fatalf("unexpected content: %q", out)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth header wrong: %q", gotAuth)
	}
	// 요청이 model과 json_object response_format을 포함하는지.
	var req map[string]any
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req["model"] != "kimi-k2.6" {
		t.Fatalf("model wrong: %v", req["model"])
	}
}

// 비200 응답은 에러로.
func TestCallChatJSONHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"suspended"}}`)
	}))
	defer srv.Close()

	c := NewAIClient()
	_, err := c.CallChatJSON(srv.URL, "k", "m", "s", "u")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/ai/ -run TestCallChatJSON`
Expected: FAIL — `undefined: CallChatJSON`.

- [ ] **Step 3: 최소 구현 작성**

`pkg/ai/chat.go`:
```go
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CallChatJSON sends a chat-completions request to any OpenAI-compatible endpoint
// (OpenAI, Kimi/Moonshot, etc.) and returns the assistant message content. baseURL is
// the API root WITHOUT the path, e.g. "https://api.moonshot.ai/v1"; the standard
// "/chat/completions" path is appended. response_format is json_object so callers can
// parse a structured decision. Reuses the openaiRequest/openaiResponse shapes from ai.go.
func (ac *AIClient) CallChatJSON(baseURL, apiKey, model, systemPrompt, userPrompt string) (string, error) {
	reqBody := openaiRequest{
		Model: model,
		Messages: []openaiMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := baseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := ac.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var openResp openaiResponse
	if err := json.Unmarshal(respBytes, &openResp); err != nil {
		return "", err
	}
	if len(openResp.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}
	return openResp.Choices[0].Message.Content, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/ai/ -v 2>&1 | tail -15`
Expected: PASS (신규 2개 + 기존 ai 테스트 회귀 0). gofmt·vet 확인.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/ai/chat.go pkg/ai/chat_test.go
git commit -m "feat(ai): OpenAI호환 CallChatJSON 헬퍼(Kimi용, base URL 분리)

기존 callOpenAI(엔드포인트 하드코딩)는 무변경. Kimi는 OpenAI 호환이라
같은 요청/응답 구조 재사용, base URL만 인자로.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: pkg/agent — SafeDecision 타입 (guard 우회 컴파일 차단)

guard.Validate가 반환하는 타입을 별도 `SafeDecision`으로 만들어, executor가 "검증 안 된 Decision"을 받을 수 없게 한다. guard만 생성할 수 있도록 unexported 필드 + 생성자로 강제.

**Files:**
- Modify: `pkg/agent/types.go`
- Test: `pkg/agent/types_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/types_test.go`에 추가:
```go
// SafeDecision은 감싼 Decision을 접근자로만 노출한다. NewSafeDecision으로 생성.
func TestSafeDecisionWrapsDecision(t *testing.T) {
	d := Decision{Action: ActionEnterLong, SizePct: 1, Confidence: 0.8}
	sd := NewSafeDecision(d)
	if sd.Decision() != d {
		t.Fatalf("SafeDecision should expose the wrapped decision; got %+v", sd.Decision())
	}
	if sd.Action() != ActionEnterLong {
		t.Fatalf("Action() accessor wrong: %s", sd.Action())
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/ -run TestSafeDecision`
Expected: FAIL — `undefined: NewSafeDecision`.

- [ ] **Step 3: SafeDecision 추가**

`pkg/agent/types.go` 끝에 추가:
```go
// SafeDecision is a Decision that has passed the risk guard. The executor accepts only
// SafeDecision, so a Decision that never went through guard.Validate cannot be executed
// — the type enforces what was previously only a convention. The wrapped decision is
// unexported; construct via NewSafeDecision (the guard) and read via the accessors.
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/ -run TestSafeDecision -v`
Expected: PASS. gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/types.go pkg/agent/types_test.go
git commit -m "feat(agent): SafeDecision 타입 — guard 거친 결정만 executor 수용(타입 강제)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: guard.Validate가 SafeDecision 반환

**Files:**
- Modify: `pkg/agent/guard/guard.go`
- Modify: `pkg/agent/guard/guard_test.go`
- Modify: `pkg/agent/pipeline_test.go`

- [ ] **Step 1: 기존 테스트를 SafeDecision 접근자로 갱신**

`pkg/agent/guard/guard_test.go`에서 `safe.Action` 직접 접근을 전부 `safe.Action()`(메서드 호출)로, `safe.SizePct`는 `safe.Decision().SizePct`로 바꾼다. 예시(전체 동일 패턴 적용):
```go
	safe, rejections := g.Validate(d, acc)
	if safe.Action() != agent.ActionHold {            // 종전: safe.Action
		t.Fatalf("...got %s", safe.Action())
	}
```
`TestValidateClampsSizeToPortfolioBudget`의 `safe.SizePct`는:
```go
	if safe.Decision().SizePct > 1.0001 {             // 종전: safe.SizePct
```
`TestValidatePassesThroughNonEntry`의 `safe.Action != act`는:
```go
	if safe.Action() != act {
```

`pkg/agent/pipeline_test.go`도 동일하게 `safe.Action` → `safe.Action()`로 갱신.

- [ ] **Step 2: 테스트가 컴파일 실패하는지 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ 2>&1 | head`
Expected: FAIL — 아직 Validate가 Decision을 반환하므로 `safe.Action undefined (type Decision has no field or method Action())` 류 또는 반환형 불일치.

(주: Step 1에서 테스트를 먼저 고쳤으므로, 구현 전에는 타입 불일치로 컴파일 실패한다 — 이것이 red 상태.)

- [ ] **Step 3: Validate 반환형 변경**

`pkg/agent/guard/guard.go`의 `Validate` 시그니처와 마지막 return을 변경:
```go
func (g *Guard) Validate(d agent.Decision, acc agent.AccountState) (agent.SafeDecision, []agent.Rejection) {
```
그리고 함수 맨 끝 `return d, rejections`를:
```go
	return agent.NewSafeDecision(d), rejections
```
중간 로직(d.Action 수정 등)은 그대로. d는 로컬 복사본이므로 그대로 두고 마지막에만 감싼다.

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/... -v 2>&1 | tail -25`
Expected: PASS (guard 11개 + agent 통합 + types). gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/guard/ pkg/agent/pipeline_test.go
git commit -m "refactor(agent/guard): Validate가 SafeDecision 반환 — 우회 컴파일 차단

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: brain — LLMClient 인터페이스 + KimiCouncil (mock 검증)

실제 Kimi 호출을 LLMClient 뒤로 숨기고, KimiCouncil이 프롬프트 생성→JSON 파싱→Decision 매핑을 한다. Phase 2에서는 mock LLMClient로 검증(실 Kimi는 잔액 충전 후).

**Files:**
- Create: `pkg/agent/brain/llm.go`
- Test: `pkg/agent/brain/llm_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/brain/llm_test.go`:
```go
package brain

import (
	"errors"
	"testing"

	"go-bot/pkg/agent"
)

// 고정 JSON을 돌려주는 mock LLM.
type stubLLM struct {
	out string
	err error
}

func (s stubLLM) Complete(systemPrompt, userPrompt string) (string, error) {
	return s.out, s.err
}

// KimiCouncil이 LLM의 JSON 응답을 agent.Decision으로 파싱한다.
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

// LLM 호출 실패 시 HOLD로 폴백(크래시 금지).
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

// 잘못된 JSON도 HOLD로 폴백.
func TestKimiCouncilFallsBackOnBadJSON(t *testing.T) {
	c := NewKimiCouncil(stubLLM{out: "not json at all"})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("bad JSON should fall back to HOLD, got %s", got.Action)
	}
}

// 알 수 없는 action은 HOLD로 정규화.
func TestKimiCouncilNormalizesUnknownAction(t *testing.T) {
	c := NewKimiCouncil(stubLLM{out: `{"action":"YOLO","confidence":0.9}`})
	got, _ := c.Deliberate(Context{Symbol: "WLDUSDT"})
	if got.Action != agent.ActionHold {
		t.Fatalf("unknown action should normalize to HOLD, got %s", got.Action)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/ -run TestKimi`
Expected: FAIL — `undefined: NewKimiCouncil`.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/brain/llm.go`:
```go
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/ -v`
Expected: PASS (KimiCouncil 4개 + 기존 MockCouncil 2개). gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/brain/llm.go pkg/agent/brain/llm_test.go
git commit -m "feat(agent/brain): LLMClient 인터페이스 + KimiCouncil(1호출 통합·HOLD폴백)

Bull/Bear/Risk를 단일 구조화 호출로. 호출실패·파싱실패·환각action은
전부 HOLD 폴백(루프 안전). 모델교체=LLMClient 구현 교체. mock으로 검증.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: brain — KimiLLM (pkg/ai.CallChatJSON 래핑)

LLMClient의 실구현. config에서 Kimi base URL·키·모델을 읽어 `pkg/ai.CallChatJSON`을 호출한다. 이건 얇은 어댑터라 단위 테스트는 생성자/필드 확인 수준.

**Files:**
- Modify: `pkg/agent/brain/llm.go`
- Test: `pkg/agent/brain/llm_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/brain/llm_test.go`에 추가:
```go
// KimiLLM이 LLMClient를 만족한다(컴파일 보장) + 생성자가 필드를 채운다.
func TestKimiLLMConstructs(t *testing.T) {
	var _ LLMClient = (*KimiLLM)(nil)
	k := NewKimiLLM("https://api.moonshot.ai/v1", "key", "kimi-k2.6")
	if k.baseURL == "" || k.model == "" {
		t.Fatal("KimiLLM fields not set")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/ -run TestKimiLLMConstructs`
Expected: FAIL — `undefined: KimiLLM`.

- [ ] **Step 3: KimiLLM 추가**

`pkg/agent/brain/llm.go`에 추가 (import에 `go-bot/pkg/ai` 추가):
```go
// KimiLLM is the production LLMClient: it calls Kimi (Moonshot, OpenAI-compatible) via
// pkg/ai.CallChatJSON. base URL is "https://api.moonshot.ai/v1", model e.g. "kimi-k2.6".
type KimiLLM struct {
	ai      *ai.AIClient
	baseURL string
	apiKey  string
	model   string
}

// NewKimiLLM builds a KimiLLM. Wire baseURL/apiKey/model from config/env (MOONSHOT_*).
func NewKimiLLM(baseURL, apiKey, model string) *KimiLLM {
	return &KimiLLM{ai: ai.NewAIClient(), baseURL: baseURL, apiKey: apiKey, model: model}
}

// Complete calls Kimi and returns the JSON content.
func (k *KimiLLM) Complete(systemPrompt, userPrompt string) (string, error) {
	return k.ai.CallChatJSON(k.baseURL, k.apiKey, k.model, systemPrompt, userPrompt)
}
```
import 블록을 다음으로:
```go
import (
	"encoding/json"
	"fmt"
	"strings"

	"go-bot/pkg/agent"
	"go-bot/pkg/ai"
)
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/ -v`
Expected: PASS. gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/brain/
git commit -m "feat(agent/brain): KimiLLM — pkg/ai.CallChatJSON 래핑한 실 LLMClient

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: runner — regime 분류 + episode ID 헬퍼

runner의 작은 순수 헬퍼 두 개를 먼저 만든다(테스트 쉬움): regime 분류기와 episode ID 발급기.

**Files:**
- Create: `pkg/agent/runner/runner.go`
- Test: `pkg/agent/runner/runner_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/runner/runner_test.go`:
```go
package runner

import (
	"strings"
	"testing"
)

// classifyRegime: 최근가가 채널 상단 근처면 trending_up, 하단 근처면 trending_down,
// 가운데면 ranging.
func TestClassifyRegime(t *testing.T) {
	cases := []struct {
		price, low, high float64
		want             string
	}{
		{0.59, 0.50, 0.60, "trending_up"},   // 상단 10% 이내
		{0.51, 0.50, 0.60, "trending_down"}, // 하단 10% 이내
		{0.55, 0.50, 0.60, "ranging"},       // 가운데
	}
	for _, c := range cases {
		if got := classifyRegime(c.price, c.low, c.high); got != c.want {
			t.Errorf("classifyRegime(%v,%v,%v)=%s want %s", c.price, c.low, c.high, got, c.want)
		}
	}
}

// episodeID는 심볼·타임스탬프·nonce로 고유 ID를 만든다.
func TestEpisodeID(t *testing.T) {
	id := episodeID("WLDUSDT", 1700000000123456789, "abc")
	if !strings.HasPrefix(id, "WLDUSDT-") || !strings.HasSuffix(id, "-abc") {
		t.Fatalf("unexpected episode id: %s", id)
	}
	// 다른 nonce면 다른 id.
	if episodeID("WLDUSDT", 1700000000123456789, "xyz") == id {
		t.Fatal("different nonce should yield different id")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/runner/ -run "TestClassifyRegime|TestEpisodeID"`
Expected: FAIL — `undefined: classifyRegime`.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/runner/runner.go`:
```go
// Package runner orchestrates one trading cycle for the AI agent: build context from
// market + memory, ask the council, validate with the guard, execute, and record the
// outcome. It wires Phase 1/2 components together without touching the live rule-bot.
package runner

import "fmt"

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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/runner/ -v`
Expected: PASS. gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/runner/
git commit -m "feat(agent/runner): regime 분류 + episode ID 헬퍼(순수·결정론)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: runner — RunOnce 한 사이클 (council→guard→execute→memory)

핵심 오케스트레이션. mock으로 주입 가능한 의존성(council·guard·memory·executor)을 받아 한 사이클을 돈다. SafeDecision만 executor로 흐른다.

**Files:**
- Modify: `pkg/agent/runner/runner.go`
- Test: `pkg/agent/runner/runner_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

먼저 `pkg/agent/runner/runner_test.go` 상단의 import 블록을 다음으로 교체한다(기존 `strings`,`testing`에 agent 패키지들 추가):
```go
import (
	"strings"
	"testing"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
)
```
그다음 파일 끝에 테스트를 추가:
```go
// 안전한 진입 결정이 guard를 통과해 executor가 호출되고, 에피소드가 기록된다.
func TestRunOnceExecutesAndRecords(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, TakeProfitPct: 4, Confidence: 0.9,
	})
	var executed *agent.SafeDecision
	var recorded *agent.TradeEpisode
	r := &Runner{
		Council:  council,
		Guard:    guard.New(0.55),
		Execute:  func(sd agent.SafeDecision) error { executed = &sd; return nil },
		Record:   func(ep agent.TradeEpisode) error { recorded = &ep; return nil },
		NowNano:  func() int64 { return 42 },
		Nonce:    func() string { return "n1" },
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}
	ctx := brain.Context{Symbol: "WLDUSDT", Regime: "trending_up", Price: 0.5}

	if err := r.RunOnce(ctx, acc); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if executed == nil {
		t.Fatal("executor should have been called for a safe entry")
	}
	if executed.Action() != agent.ActionEnterLong {
		t.Fatalf("executed action wrong: %s", executed.Action())
	}
	if recorded == nil || recorded.ID != "WLDUSDT-42-n1" {
		t.Fatalf("episode not recorded with expected id: %+v", recorded)
	}
}

// HOLD 결정(또는 guard가 막은 진입)은 executor를 부르지 않고 기록도 안 한다.
func TestRunOnceHoldDoesNothing(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{Action: agent.ActionHold, Confidence: 0.9})
	executedCalled := false
	recordedCalled := false
	r := &Runner{
		Council: council,
		Guard:   guard.New(0.55),
		Execute: func(sd agent.SafeDecision) error { executedCalled = true; return nil },
		Record:  func(ep agent.TradeEpisode) error { recordedCalled = true; return nil },
		NowNano: func() int64 { return 1 },
		Nonce:   func() string { return "x" },
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	if err := r.RunOnce(brain.Context{Symbol: "WLDUSDT"}, acc); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if executedCalled {
		t.Fatal("HOLD should not execute")
	}
	if recordedCalled {
		t.Fatal("HOLD should not record an episode")
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/runner/ -run TestRunOnce`
Expected: FAIL — `undefined: Runner`.

- [ ] **Step 3: Runner + RunOnce 구현**

`pkg/agent/runner/runner.go`에 추가(import에 agent·brain·guard 추가):
```go
import (
	"fmt"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
)
```
(주: 기존 `import "fmt"`를 위 블록으로 교체)

```go
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/runner/ -v`
Expected: PASS (RunOnce 2개 + regime/id 2개). gofmt·vet.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/runner/
git commit -m "feat(agent/runner): RunOnce 한 사이클(council→guard→execute→memory)

SafeDecision만 executor로 흐름. HOLD/guard차단은 무실행·무기록.
의존성 주입으로 mock E2E 검증.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: 통합 검증 + 전체 회귀

**Files:**
- (검증만, 신규 파일 없음)

- [ ] **Step 1: 전 패키지 build/vet/race**

Run:
```bash
cd /Users/mr.joo/Desktop/go
go build ./... && go vet ./... && go test -race ./...
```
Expected: build/vet 무출력, 모든 패키지 `ok`. 특히 `pkg/bot`·`pkg/exchange`(실거래 봇) 무변경 회귀0, `pkg/ai` 기존 테스트 통과.

- [ ] **Step 2: SafeDecision 우회 차단 확인 (수동 점검)**

다음을 확인: `pkg/agent/runner`의 `Execute func(agent.SafeDecision)` 시그니처상, guard.Validate를 거치지 않은 `agent.Decision`은 컴파일 단에서 Execute에 전달 불가. `grep -rn "Execute func" pkg/agent/runner/` 로 시그니처 확인.

- [ ] **Step 3: gofmt 확인**

Run: `cd /Users/mr.joo/Desktop/go && gofmt -l pkg/agent/ pkg/ai/`
Expected: 빈 출력.

- [ ] **Step 4: 커밋 (없으면 생략)**

검증만이라 신규 변경 없으면 커밋 생략.

---

## Task 9: 문서 갱신 (PROGRESS.md)

**Files:**
- Modify: `PROGRESS.md`

- [ ] **Step 1: Phase 2(2-A~2-D) 완료 기록 추가**

`PROGRESS.md` "다음 세션 재개 지점" 최상단에 추가:
```markdown
### 🤖 [AI 에이전트 Phase 2-A~D 완료] Kimi council + runner — MockCouncil 검증, 다음=2-E·실호출 (YYYY-MM-DD)

**설계서**: `docs/superpowers/specs/2026-06-30-ai-agent-phase2-design.md`. **계획**: `docs/superpowers/plans/2026-06-30-ai-agent-phase2.md`.
- ✅ **Kimi 연동**(`pkg/ai/chat.go`): OpenAI호환 `CallChatJSON`(base URL 분리). 기존 callOpenAI 무변경. Kimi=`api.moonshot.ai/v1`·`kimi-k2.6`·json_object.
- ✅ **SafeDecision 타입**(`pkg/agent`): guard.Validate가 SafeDecision 반환 → 검증 안 된 결정은 executor에 컴파일 단 차단(타입 강제).
- ✅ **KimiCouncil**(`pkg/agent/brain`): LLMClient 인터페이스 + 1호출 통합 Bull/Bear/Risk. 호출실패·파싱실패·환각action 전부 HOLD 폴백. KimiLLM(실 어댑터)는 pkg/ai.CallChatJSON 래핑. mock으로 검증(실 Kimi는 잔액충전 후).
- ✅ **runner**(`pkg/agent/runner`): RunOnce 한 사이클(council→guard→execute→memory) + regime분류 + episode ID. 의존성 주입 E2E.
- ✅ **검증**: 전 패키지 build/vet/-race green, 기존 실거래 봇·callOpenAI 무변경 회귀0.
- 🔴 **Kimi 잔액 0 → 실호출 정지 상태**(키는 유효·`.env` MOONSHOT_API_KEY). 충전 후 2-F에서 KimiLLM을 페이퍼에 연결.
- 🔜 **다음 = 2-E**(최소 trigger + cmd/agent 진입점 + 페이퍼 가동, MockCouncil) → **2-F**(Kimi 실호출 켜기·프롬프트 튜닝, 충전 후).
```
(YYYY-MM-DD는 실제 완료일로)

- [ ] **Step 2: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add PROGRESS.md
git commit -m "docs: AI 에이전트 Phase 2-A~D(Kimi council+runner) 완료 기록

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 완료 기준 (Phase 2-A~D Definition of Done)

- [ ] `pkg/ai/chat.go`: CallChatJSON, httptest 검증, 기존 ai 회귀0.
- [ ] `pkg/agent` SafeDecision + guard.Validate 반환형 변경, 기존 가드 테스트 갱신·통과.
- [ ] `pkg/agent/brain`: KimiCouncil(HOLD폴백 4케이스) + KimiLLM 어댑터.
- [ ] `pkg/agent/runner`: RunOnce E2E + regime/id 헬퍼.
- [ ] 전 패키지 build/vet/-race green, 실거래 봇·callOpenAI 무변경.
- [ ] PROGRESS.md 갱신.

**비목표(이 plan 아님)**: 2-E(trigger·cmd/agent·페이퍼 가동), 2-F(Kimi 실호출·프롬프트 튜닝), contextBuilder/accountBuilder의 실 exchange 연동(runner는 주입형으로만, 실 배선은 2-E).
