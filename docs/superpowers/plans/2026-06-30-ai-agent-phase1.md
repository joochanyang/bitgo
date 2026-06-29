# AI 트레이딩 에이전트 Phase 1 (토대) 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** AI 두뇌를 붙이기 전의 안전 토대 — 결정론적 리스크 가드, 결과 회고 장기기억, 모델 교체형 LLM 인터페이스를 만들고 전부 단위 테스트로 검증한다.

**Architecture:** 새 `pkg/agent/` 아래 4개 패키지(types·guard·memory·brain). guard는 기존 `pkg/strategy/sizing.go` 리스크 함수를 재사용하고, memory는 기존 `pkg/db`의 atomic JSON 영속화 패턴을 따른다. AI(LLM) 실호출은 Phase 2이며, 여기서는 brain 인터페이스 + mock 구현만 둔다. 기존 실거래 룰봇(`pkg/bot`)·실거래 봇은 전혀 건드리지 않는다 — 완전 별개 패키지.

**Tech Stack:** Go 1.25, 표준 라이브러리 + 기존 `pkg/exchange`·`pkg/strategy`·`pkg/db` 재사용. 외부 의존성 추가 없음.

---

## File Structure

- `pkg/agent/types.go` — 공통 타입(Decision·Action·AccountState·TradeEpisode·Rejection). 모든 하위 패키지가 import.
- `pkg/agent/guard/guard.go` — 결정론적 리스크 가드. `Validate(Decision, AccountState) (SafeDecision, []Rejection)`.
- `pkg/agent/guard/guard_test.go` — 가드 표 기반 단위 테스트.
- `pkg/agent/memory/memory.go` — 거래 에피소드 저장·회수·회고·통계. atomic JSON 파일.
- `pkg/agent/memory/memory_test.go` — 기억 단위 테스트(임시 디렉터리 사용).
- `pkg/agent/brain/brain.go` — `LLMClient`·`Council` 인터페이스 + `MockCouncil`(고정 결정 반환).
- `pkg/agent/brain/brain_test.go` — mock 동작 + 인터페이스 충족 테스트.

모든 파일은 단일 책임. types는 데이터만, guard는 순수 검증, memory는 영속화, brain은 추상화. 서로 인터페이스로만 의존.

---

## Task 1: 공통 타입 정의

**Files:**
- Create: `pkg/agent/types.go`
- Test: `pkg/agent/types_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/types_test.go`:
```go
package agent

import "testing"

func TestActionIsEntry(t *testing.T) {
	cases := []struct {
		a    Action
		want bool
	}{
		{ActionEnterLong, true},
		{ActionEnterShort, true},
		{ActionHold, false},
		{ActionClose, false},
		{ActionPartialClose, false},
		{ActionAdjustSL, false},
	}
	for _, c := range cases {
		if got := c.a.IsEntry(); got != c.want {
			t.Errorf("%s.IsEntry() = %v, want %v", c.a, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/ -run TestActionIsEntry`
Expected: FAIL — `undefined: Action` / build failed.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/types.go`:
```go
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
	Regime     string    `json:"regime"`   // coarse market state tag, e.g. "trending_up", "ranging"
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/ -run TestActionIsEntry -v`
Expected: PASS.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/types.go pkg/agent/types_test.go
git commit -m "feat(agent): Phase1 공통 타입(Decision·AccountState·TradeEpisode·Rejection)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: 리스크 가드 — confidence 임계 강등

**Files:**
- Create: `pkg/agent/guard/guard.go`
- Test: `pkg/agent/guard/guard_test.go`

가드는 여러 규칙을 순차 적용한다. 규칙을 하나씩 TDD로 쌓는다. 먼저 가장 단순한 규칙: confidence가 임계 미만이면 진입을 HOLD로 강등.

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/guard/guard_test.go`:
```go
package guard

import (
	"testing"

	"go-bot/pkg/agent"
)

// 진입 결정이지만 confidence가 minConfidence 미만이면 HOLD로 강등되고 사유가 남는다.
func TestValidateDowngradesLowConfidence(t *testing.T) {
	g := New(0.55) // minConfidence 0.55
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.40}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("low-confidence entry should downgrade to HOLD, got %s", safe.Action)
	}
	if len(rejections) == 0 {
		t.Fatal("expected a rejection explaining the downgrade")
	}
}

// confidence가 충분하면 진입 결정이 유지된다(다른 규칙은 통과한다고 가정).
func TestValidateKeepsConfidentEntry(t *testing.T) {
	g := New(0.55)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.80}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	safe, _ := g.Validate(d, acc)

	if safe.Action != agent.ActionEnterLong {
		t.Fatalf("confident entry should be kept, got %s", safe.Action)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -run TestValidate`
Expected: FAIL — `undefined: New` / build failed.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/guard/guard.go`:
```go
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
// plus any rejections (rule violations that were corrected). Rules are additive: each
// future task appends another check here.
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -run TestValidate -v`
Expected: PASS (두 테스트 모두).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/guard/
git commit -m "feat(agent/guard): confidence 임계 미만 진입 HOLD 강등 규칙

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: 리스크 가드 — 진입 시 SL 필수 + 잔고 확인

**Files:**
- Modify: `pkg/agent/guard/guard.go` (Validate에 규칙 추가)
- Test: `pkg/agent/guard/guard_test.go` (테스트 추가)

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/guard/guard_test.go`에 추가:
```go
// 진입인데 StopLossPct가 0이면 거부(HOLD 강등) — 손절 없는 레버리지 진입 금지.
func TestValidateRejectsEntryWithoutStopLoss(t *testing.T) {
	g := New(0.0) // confidence 규칙은 끄고 SL 규칙만 본다
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 0, Confidence: 0.9}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("entry without SL must be downgraded to HOLD, got %s", safe.Action)
	}
	if !hasRule(rejections, "stop_loss_required") {
		t.Fatalf("expected stop_loss_required rejection, got %+v", rejections)
	}
}

// 잔고 조회 실패(BalanceOK=false) 시 진입 차단.
func TestValidateBlocksEntryWhenBalanceUnknown(t *testing.T) {
	g := New(0.0)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.9}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 0, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: false}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("entry with unknown balance must be blocked, got %s", safe.Action)
	}
	if !hasRule(rejections, "balance_unknown") {
		t.Fatalf("expected balance_unknown rejection, got %+v", rejections)
	}
}

// 테스트 헬퍼: rejections에 특정 rule이 있는지.
func hasRule(rs []agent.Rejection, rule string) bool {
	for _, r := range rs {
		if r.Rule == rule {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -run "TestValidateRejectsEntryWithoutStopLoss|TestValidateBlocksEntryWhenBalanceUnknown"`
Expected: FAIL — 진입이 그대로 유지되어 HOLD가 아님.

- [ ] **Step 3: Validate에 규칙 추가**

`pkg/agent/guard/guard.go`의 `Validate` 안, confidence 규칙 다음에 추가(`return` 직전):
```go
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -v`
Expected: PASS (Task 2·3 테스트 전부).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/guard/
git commit -m "feat(agent/guard): 진입 SL 필수 + 잔고 조회 실패 시 진입 차단

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: 리스크 가드 — 포트폴리오 리스크 예산으로 sizePct 클램프

기존 `pkg/strategy/sizing.go`의 `AvailableRiskPct`를 재사용해, 진입의 SizePct가 남은 포트폴리오 리스크 예산을 넘지 않게 클램프한다.

**Files:**
- Modify: `pkg/agent/guard/guard.go`
- Test: `pkg/agent/guard/guard_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/guard/guard_test.go`에 추가:
```go
// 이미 다른 포지션이 리스크 예산 대부분을 쓰고 있으면, 새 진입의 SizePct가
// 남은 예산으로 클램프된다. balance 50, maxPortfolioRisk 10% => 예산 5 USDT.
// committed 4.5 USDT면 남은 0.5 USDT = balance의 1%. 요청 3% -> 1%로 깎임.
func TestValidateClampsSizeToPortfolioBudget(t *testing.T) {
	g := New(0.0)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 3, StopLossPct: 2, Confidence: 0.9}
	acc := agent.AccountState{
		Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3,
		CommittedRiskUSDT: 4.5, MaxPortfolioRisk: 10, BalanceOK: true,
	}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionEnterLong {
		t.Fatalf("entry should remain, got %s", safe.Action)
	}
	if safe.SizePct > 1.0001 {
		t.Fatalf("sizePct should be clamped to ~1%%, got %v", safe.SizePct)
	}
	if !hasRule(rejections, "portfolio_risk_clamp") {
		t.Fatalf("expected portfolio_risk_clamp rejection, got %+v", rejections)
	}
}

// 예산이 완전히 소진되었으면(남은 0) 진입 자체를 HOLD로.
func TestValidateBlocksEntryWhenBudgetExhausted(t *testing.T) {
	g := New(0.0)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 2, StopLossPct: 2, Confidence: 0.9}
	acc := agent.AccountState{
		Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3,
		CommittedRiskUSDT: 5.0, MaxPortfolioRisk: 10, BalanceOK: true, // 예산 5, 소진
	}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("exhausted budget should block entry, got %s", safe.Action)
	}
	if !hasRule(rejections, "portfolio_risk_clamp") {
		t.Fatalf("expected portfolio_risk_clamp rejection, got %+v", rejections)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -run "TestValidateClampsSizeToPortfolioBudget|TestValidateBlocksEntryWhenBudgetExhausted"`
Expected: FAIL.

- [ ] **Step 3: Validate에 클램프 규칙 추가**

`pkg/agent/guard/guard.go` 상단 import에 strategy 추가:
```go
import (
	"go-bot/pkg/agent"
	"go-bot/pkg/strategy"
)
```

`Validate` 안, 잔고 규칙 다음에 추가(`return` 직전):
```go
	// Rule: clamp entry size to the remaining portfolio risk budget. Reuses the live
	// bot's AvailableRiskPct so the AI agent obeys the same 10% portfolio cap. If the
	// budget is exhausted (0 available), the entry is downgraded to HOLD.
	if d.Action.IsEntry() {
		avail := strategy.AvailableRiskPct(acc.Balance, acc.MaxPortfolioRisk, acc.CommittedRiskUSDT, d.SizePct)
		if avail <= 0 {
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -v`
Expected: PASS (전체).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/guard/
git commit -m "feat(agent/guard): 포트폴리오 리스크 예산으로 진입 size 클램프(sizing 재사용)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: 리스크 가드 — 소액계좌 최소주문수량 보정

감사에서 발견한 문제: 소액계좌에서 최소주문수량 때문에 의도한 risk%보다 큰 포지션이 강제될 수 있음. 진입 결정의 실제 수량이 minOrderQty 미만이면 진입 불가로 표시(HOLD 강등)해, "의도보다 큰 포지션 강제"를 막는다.

**Files:**
- Modify: `pkg/agent/guard/guard.go`
- Test: `pkg/agent/guard/guard_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/guard/guard_test.go`에 추가:
```go
// 소액계좌 + 고가코인: risk 기반 수량이 거래소 최소수량 미만이면, 최소수량으로
// 강제 진입하면 risk%가 의도보다 커지므로 진입을 HOLD로 막는다.
// balance 50, risk 1%(=0.5 USDT), SL 2%, BTC price 60000, minOrderQty 0.001.
// slDistancePerUnit = 60000*0.02 = 1200. riskQty = 0.5/1200 = 0.000417 < 0.001 -> 막힘.
func TestValidateBlocksWhenQtyBelowMinOrder(t *testing.T) {
	g := New(0.0)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.9}
	acc := agent.AccountState{
		Symbol: "BTCUSDT", Balance: 50, Price: 60000, MinOrderQty: 0.001, Leverage: 3,
		MaxPortfolioRisk: 10, BalanceOK: true,
	}

	safe, rejections := g.Validate(d, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("entry below min order qty should be blocked, got %s", safe.Action)
	}
	if !hasRule(rejections, "below_min_order_qty") {
		t.Fatalf("expected below_min_order_qty rejection, got %+v", rejections)
	}
}

// 저가코인은 같은 risk로도 수량이 최소수량을 넘으므로 진입 유지.
// WLD price 0.5, risk 0.5 USDT, SL 2% -> slDist=0.01, qty=50 >> minOrderQty 1.
func TestValidateAllowsEntryWhenQtyAboveMin(t *testing.T) {
	g := New(0.0)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.9}
	acc := agent.AccountState{
		Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3,
		MaxPortfolioRisk: 10, BalanceOK: true,
	}

	safe, _ := g.Validate(d, acc)

	if safe.Action != agent.ActionEnterLong {
		t.Fatalf("low-price entry should remain, got %s", safe.Action)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -run "TestValidateBlocksWhenQtyBelowMinOrder|TestValidateAllowsEntryWhenQtyAboveMin"`
Expected: FAIL.

- [ ] **Step 3: Validate에 최소수량 규칙 추가**

`pkg/agent/guard/guard.go`의 `Validate` 안, 포트폴리오 클램프 규칙 다음에 추가(`return` 직전). `strategy.RiskBasedQty`로 실제 수량을 계산해 minOrderQty와 비교:
```go
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/guard/ -v`
Expected: PASS (전체).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/guard/
git commit -m "feat(agent/guard): 소액계좌 최소주문수량 미달 진입 차단(오버사이징 방지)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: 장기기억 — 에피소드 저장·조회

기존 `pkg/db/db.go`의 atomic JSON 파일 패턴을 따라, 거래 에피소드를 JSON 파일에 누적한다. memory는 파일 경로를 주입받아(테스트 격리) 동작한다.

**Files:**
- Create: `pkg/agent/memory/memory.go`
- Test: `pkg/agent/memory/memory_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/memory/memory_test.go`:
```go
package memory

import (
	"path/filepath"
	"testing"
	"time"

	"go-bot/pkg/agent"
)

func tmpStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "episodes.json")
	return New(path)
}

// Record한 에피소드가 All로 다시 읽힌다(파일 영속화 라운드트립).
func TestRecordAndAll(t *testing.T) {
	s := tmpStore(t)
	ep := agent.TradeEpisode{
		ID: "e1", OpenedAt: time.Unix(1000, 0), Symbol: "WLDUSDT",
		Regime: "trending_up", EntryPrice: 0.5,
		Decision: agent.Decision{Action: agent.ActionEnterLong, Confidence: 0.7},
	}
	if err := s.Record(ep); err != nil {
		t.Fatalf("Record: %v", err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ID != "e1" {
		t.Fatalf("expected 1 episode e1, got %+v", all)
	}
}

// 빈 저장소는 빈 슬라이스를 반환(에러 아님).
func TestAllEmpty(t *testing.T) {
	s := tmpStore(t)
	all, err := s.All()
	if err != nil {
		t.Fatalf("All on empty: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty, got %+v", all)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -run "TestRecordAndAll|TestAllEmpty"`
Expected: FAIL — `undefined: New` / build failed.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/memory/memory.go`:
```go
// Package memory is the agent's long-term memory: it persists every trade episode
// (situation, decision, and — after close — outcome) so the council can recall similar
// past trades and learn from results. Backed by an atomic JSON file, mirroring pkg/db.
package memory

import (
	"encoding/json"
	"os"

	"go-bot/pkg/agent"
)

// Store is a file-backed episode store. Construct with New.
type Store struct {
	path string
}

// New returns a Store persisting to path. The file is created on first Record.
func New(path string) *Store {
	return &Store{path: path}
}

// All returns every recorded episode, oldest first. An absent file is not an error
// (returns empty), matching pkg/db's loadTradesRaw behaviour.
func (s *Store) All() ([]agent.TradeEpisode, error) {
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return []agent.TradeEpisode{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []agent.TradeEpisode{}, nil
	}
	var eps []agent.TradeEpisode
	if err := json.Unmarshal(data, &eps); err != nil {
		return nil, err
	}
	return eps, nil
}

// Record appends an episode and persists atomically (write temp + rename), the same
// crash-safe pattern pkg/db uses for trades.json.
func (s *Store) Record(ep agent.TradeEpisode) error {
	eps, err := s.All()
	if err != nil {
		return err
	}
	eps = append(eps, ep)
	return s.writeAll(eps)
}

func (s *Store) writeAll(eps []agent.TradeEpisode) error {
	data, err := json.MarshalIndent(eps, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -v`
Expected: PASS.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/memory/
git commit -m "feat(agent/memory): 거래 에피소드 atomic JSON 저장·조회

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: 장기기억 — 유사 사례 회수(Recall)

새 판단 시 현재 상황과 유사한 과거 에피소드를 회수한다. Phase 1은 규칙 기반: 같은 심볼 + 같은 regime을 우선, 최근 것부터 최대 K건.

**Files:**
- Modify: `pkg/agent/memory/memory.go`
- Test: `pkg/agent/memory/memory_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/memory/memory_test.go`에 추가:
```go
// Recall은 같은 심볼+같은 regime 에피소드를 최근 순으로 최대 k건 반환한다.
func TestRecallMatchesSymbolAndRegime(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "e1", "WLDUSDT", "trending_up", 100)
	mustRecord(t, s, "e2", "BTCUSDT", "trending_up", 200) // 다른 심볼 -> 제외
	mustRecord(t, s, "e3", "WLDUSDT", "ranging", 300)      // 다른 regime -> 제외
	mustRecord(t, s, "e4", "WLDUSDT", "trending_up", 400)

	got := s.Recall("WLDUSDT", "trending_up", 10)

	if len(got) != 2 {
		t.Fatalf("expected 2 matches (e1,e4), got %d: %+v", len(got), got)
	}
	// 최근 순(e4가 먼저).
	if got[0].ID != "e4" || got[1].ID != "e1" {
		t.Fatalf("expected recency order e4,e1; got %s,%s", got[0].ID, got[1].ID)
	}
}

// k 제한이 적용된다.
func TestRecallRespectsLimit(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "a", "WLDUSDT", "ranging", 1)
	mustRecord(t, s, "b", "WLDUSDT", "ranging", 2)
	mustRecord(t, s, "c", "WLDUSDT", "ranging", 3)

	got := s.Recall("WLDUSDT", "ranging", 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 (limit), got %d", len(got))
	}
	if got[0].ID != "c" || got[1].ID != "b" {
		t.Fatalf("expected most-recent c,b; got %s,%s", got[0].ID, got[1].ID)
	}
}

func mustRecord(t *testing.T, s *Store, id, sym, regime string, ts int64) {
	t.Helper()
	if err := s.Record(agent.TradeEpisode{ID: id, Symbol: sym, Regime: regime, OpenedAt: time.Unix(ts, 0)}); err != nil {
		t.Fatalf("Record %s: %v", id, err)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -run TestRecall`
Expected: FAIL — `undefined: Recall` / build failed.

- [ ] **Step 3: Recall 구현 추가**

`pkg/agent/memory/memory.go`에 추가(import에 `sort` 추가):
```go
// Recall returns up to k past episodes matching the given symbol and regime, most
// recent first. Phase 1 uses simple rule-based matching; embedding similarity is a
// later enhancement. On read error it returns nil (caller treats memory as empty).
func (s *Store) Recall(symbol, regime string, k int) []agent.TradeEpisode {
	all, err := s.All()
	if err != nil {
		return nil
	}
	var matches []agent.TradeEpisode
	for _, ep := range all {
		if ep.Symbol == symbol && ep.Regime == regime {
			matches = append(matches, ep)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].OpenedAt.After(matches[j].OpenedAt)
	})
	if k > 0 && len(matches) > k {
		matches = matches[:k]
	}
	return matches
}
```

import 블록을 다음으로 변경(`sort` 추가):
```go
import (
	"encoding/json"
	"os"
	"sort"

	"go-bot/pkg/agent"
)
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -v`
Expected: PASS (전체).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/memory/
git commit -m "feat(agent/memory): 규칙기반 유사사례 회수(Recall) — 심볼+regime 최근순

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: 장기기억 — 회고 업데이트(청산 결과 채우기) + 통계

청산 시 해당 에피소드에 결과(PnL·청산사유)를 채워 "그 판단이 옳았나"를 사후 라벨링하고, 누적 통계(승률·표본수)를 낸다.

**Files:**
- Modify: `pkg/agent/memory/memory.go`
- Test: `pkg/agent/memory/memory_test.go`

- [ ] **Step 1: 실패하는 테스트 추가**

`pkg/agent/memory/memory_test.go`에 추가:
```go
// Close는 해당 ID 에피소드에 결과를 채우고 closed=true로 만든다.
func TestCloseFillsOutcome(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "e1", "WLDUSDT", "trending_up", 100)

	if err := s.Close("e1", time.Unix(500, 0), 0.55, 10.0, "tp"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	all, _ := s.All()
	if !all[0].Closed || all[0].PnLPct != 10.0 || all[0].ExitReason != "tp" {
		t.Fatalf("episode not closed correctly: %+v", all[0])
	}
}

// Stats는 닫힌 거래의 표본수·승률(PnL>0 비율)을 계산한다.
func TestStatsWinRate(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "w1", "WLDUSDT", "x", 1)
	mustRecord(t, s, "w2", "WLDUSDT", "x", 2)
	mustRecord(t, s, "l1", "WLDUSDT", "x", 3)
	mustRecord(t, s, "open", "WLDUSDT", "x", 4) // 미청산 -> 통계 제외
	mustClose(t, s, "w1", 5.0)
	mustClose(t, s, "w2", 3.0)
	mustClose(t, s, "l1", -4.0)

	st := s.Stats()
	if st.Closed != 3 {
		t.Fatalf("expected 3 closed, got %d", st.Closed)
	}
	if st.Wins != 2 {
		t.Fatalf("expected 2 wins, got %d", st.Wins)
	}
	if st.WinRate < 0.66 || st.WinRate > 0.67 {
		t.Fatalf("expected winRate ~0.667, got %v", st.WinRate)
	}
}

func mustClose(t *testing.T, s *Store, id string, pnlPct float64) {
	t.Helper()
	if err := s.Close(id, time.Unix(999, 0), 0, pnlPct, "test"); err != nil {
		t.Fatalf("Close %s: %v", id, err)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -run "TestCloseFillsOutcome|TestStatsWinRate"`
Expected: FAIL — `undefined: Close` / `undefined: Stats`.

- [ ] **Step 3: Close·Stats 구현 추가**

`pkg/agent/memory/memory.go`에 추가:
```go
// PerformanceSummary is the aggregate over closed episodes, used by the performance
// tracker to decide when to suggest a stage upgrade (paper -> live -> larger size).
type PerformanceSummary struct {
	Closed  int     `json:"closed"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"win_rate"` // wins / closed, 0 when none closed
	AvgPnL  float64 `json:"avg_pnl"`  // mean PnLPct over closed
}

// Close fills in the retrospective outcome for the episode with the given id and
// persists. This is how a decision gets labelled right/wrong after the fact, so future
// Recalls carry the lesson. Returns an error if the id is not found.
func (s *Store) Close(id string, closedAt time.Time, exitPrice, pnlPct float64, reason string) error {
	eps, err := s.All()
	if err != nil {
		return err
	}
	found := false
	for i := range eps {
		if eps[i].ID == id {
			eps[i].Closed = true
			eps[i].ClosedAt = closedAt
			eps[i].ExitPrice = exitPrice
			eps[i].PnLPct = pnlPct
			eps[i].ExitReason = reason
			found = true
			break
		}
	}
	if !found {
		return os.ErrNotExist
	}
	return s.writeAll(eps)
}

// Stats aggregates closed episodes. Open episodes are ignored. On read error it
// returns a zero summary.
func (s *Store) Stats() PerformanceSummary {
	all, err := s.All()
	if err != nil {
		return PerformanceSummary{}
	}
	var sum PerformanceSummary
	var pnlTotal float64
	for _, ep := range all {
		if !ep.Closed {
			continue
		}
		sum.Closed++
		pnlTotal += ep.PnLPct
		if ep.PnLPct > 0 {
			sum.Wins++
		}
	}
	if sum.Closed > 0 {
		sum.WinRate = float64(sum.Wins) / float64(sum.Closed)
		sum.AvgPnL = pnlTotal / float64(sum.Closed)
	}
	return sum
}
```

import에 `time` 추가:
```go
import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"go-bot/pkg/agent"
)
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/memory/ -v`
Expected: PASS (전체).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/memory/
git commit -m "feat(agent/memory): 회고 업데이트(Close) + 누적 통계(Stats 승률·평균PnL)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 9: LLM 추상화 — 인터페이스 + MockCouncil

Phase 2에서 실제 Bull/Bear/Risk를 붙일 자리를 인터페이스로 정의하고, 모델에 종속되지 않게 한다. Phase 1에서는 실 LLM 호출 없이 고정 결정을 반환하는 MockCouncil만 구현해, runner/통합테스트가 AI 비용 없이 파이프라인을 검증할 수 있게 한다.

**Files:**
- Create: `pkg/agent/brain/brain.go`
- Test: `pkg/agent/brain/brain_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`pkg/agent/brain/brain_test.go`:
```go
package brain

import (
	"testing"

	"go-bot/pkg/agent"
)

// MockCouncil은 주입한 고정 결정을 그대로 반환한다 — AI 없이 파이프라인 검증용.
func TestMockCouncilReturnsFixedDecision(t *testing.T) {
	want := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.8}
	var c Council = NewMockCouncil(want)

	got, err := c.Deliberate(Context{Symbol: "WLDUSDT", Regime: "trending_up"})
	if err != nil {
		t.Fatalf("Deliberate: %v", err)
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

// MockCouncil은 Council 인터페이스를 만족한다(컴파일 타임 보장).
func TestMockCouncilSatisfiesInterface(t *testing.T) {
	var _ Council = (*MockCouncil)(nil)
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/`
Expected: FAIL — `undefined: Council` / build failed.

- [ ] **Step 3: 최소 구현 작성**

`pkg/agent/brain/brain.go`:
```go
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
	Symbol  string
	Regime  string
	Price   float64
	Past    []agent.TradeEpisode // recalled by memory
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
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/brain/ -v`
Expected: PASS.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/brain/
git commit -m "feat(agent/brain): Council 인터페이스 + MockCouncil(모델 추상화, Phase2 자리)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 10: 통합 검증 — guard+brain 파이프라인 + 전체 회귀

brain의 MockCouncil 결정이 guard를 통과하는 한 사이클을 통합 테스트로 묶고, 전 패키지 회귀를 확인한다.

**Files:**
- Create: `pkg/agent/pipeline_test.go`

- [ ] **Step 1: 통합 테스트 작성**

`pkg/agent/pipeline_test.go`:
```go
package agent_test

import (
	"testing"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
)

// MockCouncil이 위험한 진입(SL 없음)을 내도 guard가 HOLD로 막는다 — 한 사이클 검증.
func TestPipelineGuardBlocksUnsafeCouncilDecision(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 0, Confidence: 0.9, // SL 없음
	})
	g := guard.New(0.55)

	decision, err := council.Deliberate(brain.Context{Symbol: "WLDUSDT", Regime: "trending_up"})
	if err != nil {
		t.Fatalf("Deliberate: %v", err)
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	safe, rejections := g.Validate(decision, acc)

	if safe.Action != agent.ActionHold {
		t.Fatalf("guard must block SL-less entry, got %s", safe.Action)
	}
	if len(rejections) == 0 {
		t.Fatal("expected rejection from guard")
	}
}

// 안전한 진입 결정은 파이프라인을 통과한다.
func TestPipelineAllowsSafeDecision(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.9,
	})
	g := guard.New(0.55)

	decision, _ := council.Deliberate(brain.Context{Symbol: "WLDUSDT", Regime: "trending_up"})
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	safe, _ := g.Validate(decision, acc)
	if safe.Action != agent.ActionEnterLong {
		t.Fatalf("safe entry should pass, got %s", safe.Action)
	}
}
```

- [ ] **Step 2: 통합 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./pkg/agent/... -v`
Expected: PASS (전체 agent 트리).

- [ ] **Step 3: 전 패키지 빌드·vet·race 회귀 확인**

Run:
```bash
cd /Users/mr.joo/Desktop/go
go build ./... && go vet ./... && go test -race ./...
```
Expected: build/vet 무출력(성공), 모든 패키지 `ok` (기존 패키지 포함 회귀 0). 특히 `pkg/bot`·`pkg/exchange`(실거래 봇) 변경 없음 확인.

- [ ] **Step 4: gofmt 확인**

Run: `cd /Users/mr.joo/Desktop/go && gofmt -l pkg/agent/`
Expected: 빈 출력(포맷 OK).

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add pkg/agent/pipeline_test.go
git commit -m "test(agent): guard+council 파이프라인 통합 테스트 + Phase1 토대 완료

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 11: 문서 갱신 (PROGRESS.md)

**Files:**
- Modify: `PROGRESS.md`

- [ ] **Step 1: PROGRESS.md에 Phase 1 완료 기록 추가**

`PROGRESS.md`의 "다음 세션 재개 지점" 섹션 최상단에 추가:
```markdown
### 🤖 [AI 에이전트 Phase 1 완료] 토대(guard·memory·brain) — 페이퍼 전, 다음=Phase 2 (YYYY-MM-DD)

**설계서**: `docs/superpowers/specs/2026-06-30-ai-trading-agent-design.md`. **계획**: `docs/superpowers/plans/2026-06-30-ai-agent-phase1.md`.
- ✅ **새 `pkg/agent/` (실거래 룰봇과 완전 별개)**: types(Decision·AccountState·TradeEpisode·Rejection) + guard(결정론적 리스크가드) + memory(결과회고 장기기억) + brain(Council 인터페이스+MockCouncil).
- ✅ **guard 규칙**(순수코드, AI 우회불가): confidence임계 강등·진입SL필수·잔고실패차단·포트폴리오리스크 클램프(sizing재사용)·소액계좌 최소수량 미달차단. 표기반 단위테스트.
- ✅ **memory**: 에피소드 atomic JSON 저장·규칙기반 Recall(심볼+regime 최근순)·회고 Close(결과채움)·Stats(승률·평균PnL). db패턴 재사용.
- ✅ **brain**: 모델 추상화(Council 인터페이스). Phase1은 MockCouncil만(AI 비용0). 실 Bull/Bear/Risk=Phase2.
- ✅ **검증**: 전 패키지 build/vet/`-race` green, 기존 실거래 봇(pkg/bot·pkg/exchange) 무변경 회귀0.
- 🔜 **다음 = Phase 2**: council 실구현(Bull/Bear 병렬 + Risk 종합, LLM 연결) + runner(파이프라인 엮기). 모델 선택 필요(키 발급).
```
(YYYY-MM-DD는 실제 완료일로 치환)

- [ ] **Step 2: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add PROGRESS.md
git commit -m "docs: AI 에이전트 Phase1(토대) 완료 기록

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 완료 기준 (Phase 1 Definition of Done)

- [ ] `pkg/agent/` 4개 패키지(types·guard·memory·brain) 생성, 각 단일 책임.
- [ ] guard: 5개 리스크 규칙 전부 표기반 테스트 통과.
- [ ] memory: 저장·회수·회고·통계 테스트 통과.
- [ ] brain: Council 인터페이스 + MockCouncil, 인터페이스 충족 컴파일 보장.
- [ ] 통합 테스트: guard가 위험 결정 차단 검증.
- [ ] 전 패키지 build/vet/`-race` green, 기존 실거래 봇 무변경.
- [ ] PROGRESS.md 갱신.

**비목표(Phase 1 아님)**: 실제 LLM 호출, 이벤트 트리거, runner 오케스트레이션, 페이퍼 실행, 성과 전환 제안 — 전부 Phase 2+.
