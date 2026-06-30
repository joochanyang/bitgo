# AI 트레이딩 에이전트 Phase 2-E 구현 계획 — cmd/agent 페이퍼 가동

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Phase 2-A~D 부품(council→guard→execute→memory)을 실제로 돌리는 독립 실행파일 `cmd/agent`를 만들어, DeepSeek(키 있으면) 또는 MockCouncil(없으면)로 페이퍼 사이클을 가동한다.

**Architecture:** 모든 신규 코드는 `cmd/agent/` 안에 둔다(기존 `pkg/*`는 읽기 전용 재사용, 실거래 봇 무변경). config 재사용·exchange 공개데이터·council 환경변수 자동선택·페이퍼 executor(주문0·로그/메모리만). `time.Ticker` 틱 루프가 심볼별로 `buildContext`→`runner.RunOnce`를 돈다.

**Tech Stack:** Go 1.25, 표준 라이브러리 + 기존 `pkg/config`·`pkg/exchange`·`pkg/strategy`·`pkg/agent`(+brain·guard·memory·runner) 재사용. 외부 의존성 추가 없음.

---

## File Structure

- `cmd/agent/council.go` (신규) — `pickCouncil(env)` council 자동선택(순수, env 주입).
- `cmd/agent/council_test.go` (신규) — 키 유무 분기 테스트.
- `cmd/agent/regime.go` (신규) — `classifyRegime`·`tickInterval` 순수 헬퍼.
- `cmd/agent/regime_test.go` (신규) — regime 분류·interval 변환 테스트.
- `cmd/agent/context.go` (신규) — `buildContext(ex, symbol, mem, k)`.
- `cmd/agent/context_test.go` (신규) — stub exchange로 regime·price·recall 검증.
- `cmd/agent/account.go` (신규) — `buildAccount(ex, symbol, allSymbols, cfg)`.
- `cmd/agent/account_test.go` (신규) — balance 실패·committed risk 합산 검증.
- `cmd/agent/main.go` (신규) — 진입점·틱 루프·executor·OnReject 배선.
- `.gitignore` (수정) — `agent_memory.json` 추가.

테스트용 stub exchange는 `context_test.go`에 정의하고 `account_test.go`가 공유한다(같은 패키지 `main`).

---

## Task 1: regime/interval 순수 헬퍼

runner의 `classifyRegime`은 비공개라 재사용 불가 → cmd/agent에 **동일 기준**으로 복제(테스트로 박제). `tickInterval`은 config interval 문자열을 틱 주기로 변환.

**Files:**
- Create: `cmd/agent/regime.go`
- Test: `cmd/agent/regime_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/agent/regime_test.go`:
```go
package main

import (
	"testing"
	"time"
)

func TestClassifyRegime(t *testing.T) {
	cases := []struct {
		price, low, high float64
		want             string
	}{
		{0.59, 0.50, 0.60, "trending_up"},   // 상단 10% 이내
		{0.51, 0.50, 0.60, "trending_down"}, // 하단 10% 이내
		{0.55, 0.50, 0.60, "ranging"},       // 가운데
		{0.5, 0.6, 0.6, "ranging"},          // high<=low 방어
	}
	for _, c := range cases {
		if got := classifyRegime(c.price, c.low, c.high); got != c.want {
			t.Errorf("classifyRegime(%v,%v,%v)=%s want %s", c.price, c.low, c.high, got, c.want)
		}
	}
}

func TestTickInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"4h", 4 * time.Hour},
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"15m", 15 * time.Minute},
		{"5m", 5 * time.Minute},
		{"bogus", 4 * time.Hour}, // 알 수 없으면 기본 4h
		{"", 4 * time.Hour},
	}
	for _, c := range cases {
		if got := tickInterval(c.in); got != c.want {
			t.Errorf("tickInterval(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run "TestClassifyRegime|TestTickInterval"`
Expected: FAIL — `undefined: classifyRegime`.

- [ ] **Step 3: 최소 구현 작성**

`cmd/agent/regime.go`:
```go
// Command agent is the standalone AI trading agent: it runs the council -> guard ->
// execute -> memory cycle on a timer, in paper mode (no real orders). It is fully
// separate from the live rule-bot (pkg/bot) and its web dashboard.
package main

import "time"

// classifyRegime tags the market state from where price sits in the [low, high] channel.
// Within 10% of the top -> trending_up, within 10% of the bottom -> trending_down, else
// ranging. This MUST stay identical to runner.classifyRegime so memory recall keys match.
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

// tickInterval maps a config interval string to the loop period. Unknown values fall
// back to 4h (the validated default timeframe for the breakout edge).
func tickInterval(interval string) time.Duration {
	switch interval {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		return 4 * time.Hour
	}
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -v 2>&1 | tail -15`
Expected: PASS. `gofmt -l cmd/agent/` 빈 출력, `go vet ./cmd/agent/` 무출력.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add cmd/agent/regime.go cmd/agent/regime_test.go
git commit -m "feat(cmd/agent): regime 분류 + tickInterval 순수 헬퍼

runner.classifyRegime과 동일 기준 복제(recall 키 정합). 테스트로 박제.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: council 자동 선택

`DEEPSEEK_API_KEY`가 있으면 실 DeepSeek(LLMCouncil + OpenAICompatLLM), 없으면 MockCouncil. env 조회를 주입해 순수하게 테스트.

**Files:**
- Create: `cmd/agent/council.go`
- Test: `cmd/agent/council_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/agent/council_test.go`:
```go
package main

import (
	"testing"

	"go-bot/pkg/agent/brain"
)

func TestPickCouncilDeepSeek(t *testing.T) {
	env := func(k string) string {
		if k == "DEEPSEEK_API_KEY" {
			return "sk-test"
		}
		return ""
	}
	c, label := pickCouncil(env)
	if label != "deepseek" {
		t.Fatalf("label = %q, want deepseek", label)
	}
	if _, ok := c.(*brain.LLMCouncil); !ok {
		t.Fatalf("expected *brain.LLMCouncil, got %T", c)
	}
}

func TestPickCouncilFallsBackToMock(t *testing.T) {
	env := func(string) string { return "" } // no key
	c, label := pickCouncil(env)
	if label != "mock" {
		t.Fatalf("label = %q, want mock", label)
	}
	if _, ok := c.(*brain.MockCouncil); !ok {
		t.Fatalf("expected *brain.MockCouncil, got %T", c)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestPickCouncil`
Expected: FAIL — `undefined: pickCouncil`.

- [ ] **Step 3: 최소 구현 작성**

`cmd/agent/council.go`:
```go
package main

import (
	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
)

// DeepSeek defaults (OpenAI-compatible). Overridable via env DEEPSEEK_BASE_URL/DEEPSEEK_MODEL.
const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com/v1"
	defaultDeepSeekModel   = "deepseek-v4-flash"
)

// pickCouncil chooses the council from the environment: a real DeepSeek-backed LLMCouncil
// when DEEPSEEK_API_KEY is set, otherwise a MockCouncil that always HOLDs (zero cost, safe
// for wiring/dry runs). env is injected so the choice is unit-testable. Returns the council
// and a short label for the startup log.
func pickCouncil(env func(string) string) (brain.Council, string) {
	key := env("DEEPSEEK_API_KEY")
	if key == "" {
		return brain.NewMockCouncil(agent.Decision{Action: agent.ActionHold, Reasoning: "no DEEPSEEK_API_KEY: mock council"}), "mock"
	}
	baseURL := env("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	model := env("DEEPSEEK_MODEL")
	if model == "" {
		model = defaultDeepSeekModel
	}
	llm := brain.NewOpenAICompatLLM(baseURL, key, model)
	return brain.NewLLMCouncil(llm), "deepseek"
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestPickCouncil -v`
Expected: PASS. gofmt·vet 클린.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add cmd/agent/council.go cmd/agent/council_test.go
git commit -m "feat(cmd/agent): council 환경변수 자동선택(DeepSeek 있으면 실, 없으면 Mock)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: buildContext (stub exchange 테스트)

최근 캔들로 채널·regime·price를 만들고 memory.Recall로 Past를 채운다. 테스트용 stub exchange를 여기서 정의(Task 4 account_test가 공유).

**Files:**
- Create: `cmd/agent/context.go`
- Test: `cmd/agent/context_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/agent/context_test.go`:
```go
package main

import (
	"testing"
	"time"

	"go-bot/pkg/exchange"
)

// stubExchange implements exchange.Exchange with settable fields for tests. Only the
// methods buildContext/buildAccount call do anything; the rest satisfy the interface.
type stubExchange struct {
	klines    []exchange.Candle
	klinesErr error
	ticker    float64
	tickerErr error
	balance   float64
	balanceErr error
	positions map[string]*exchange.Position
}

func (s *stubExchange) GetTicker(string) (float64, error) { return s.ticker, s.tickerErr }
func (s *stubExchange) GetKlines(string, string, int) ([]exchange.Candle, error) {
	return s.klines, s.klinesErr
}
func (s *stubExchange) GetKlinesPaged(string, string, int) ([]exchange.Candle, error) {
	return s.klines, s.klinesErr
}
func (s *stubExchange) GetBalance() (float64, error) { return s.balance, s.balanceErr }
func (s *stubExchange) GetPosition(sym string) (*exchange.Position, error) {
	if s.positions == nil {
		return &exchange.Position{Symbol: sym, Side: "NONE"}, nil
	}
	if p, ok := s.positions[sym]; ok {
		return p, nil
	}
	return &exchange.Position{Symbol: sym, Side: "NONE"}, nil
}
func (s *stubExchange) PlaceOrder(string, string, float64, float64, exchange.OrderOptions) (*exchange.OrderResult, error) {
	return nil, nil
}
func (s *stubExchange) ClosePosition(string) error          { return nil }
func (s *stubExchange) SetLeverage(string, int) error       { return nil }
func (s *stubExchange) SetStopLoss(string, float64) error   { return nil }

// candles builds a rising series so the last close sits near the channel top.
func risingCandles(n int) []exchange.Candle {
	out := make([]exchange.Candle, n)
	base := time.Unix(1700000000, 0)
	for i := 0; i < n; i++ {
		price := 0.50 + float64(i)*0.005
		out[i] = exchange.Candle{
			Time: base.Add(time.Duration(i) * time.Hour),
			Open: price, High: price + 0.002, Low: price - 0.002, Close: price, Volume: 100,
		}
	}
	return out
}

func TestBuildContextClassifiesAndPrices(t *testing.T) {
	ex := &stubExchange{klines: risingCandles(40)}
	ctx, err := buildContext(ex, "WLDUSDT", "4h", nil, 3)
	if err != nil {
		t.Fatalf("buildContext: %v", err)
	}
	if ctx.Symbol != "WLDUSDT" {
		t.Fatalf("symbol = %q", ctx.Symbol)
	}
	if ctx.Regime != "trending_up" {
		t.Fatalf("regime = %q, want trending_up (rising series)", ctx.Regime)
	}
	last := ex.klines[len(ex.klines)-1].Close
	if ctx.Price != last {
		t.Fatalf("price = %v, want last close %v", ctx.Price, last)
	}
}

func TestBuildContextErrorsOnFetchFailure(t *testing.T) {
	ex := &stubExchange{klinesErr: errFetch}
	if _, err := buildContext(ex, "WLDUSDT", "4h", nil, 3); err == nil {
		t.Fatal("expected error when kline fetch fails")
	}
}

func TestBuildContextErrorsOnTooFewCandles(t *testing.T) {
	ex := &stubExchange{klines: risingCandles(5)} // fewer than lookback
	if _, err := buildContext(ex, "WLDUSDT", "4h", nil, 3); err == nil {
		t.Fatal("expected error when not enough candles")
	}
}
```

(주: `errFetch`는 Step 3에서 정의. nil memory는 `buildContext`가 nil-safe하게 처리 — recall 생략.)

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestBuildContext`
Expected: FAIL — `undefined: buildContext` / `undefined: errFetch`.

- [ ] **Step 3: 최소 구현 작성**

`cmd/agent/context.go`:
```go
package main

import (
	"errors"
	"fmt"

	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/memory"
	"go-bot/pkg/exchange"
)

// contextLookback is how many prior candles define the breakout channel for regime
// classification (matches the live breakout strategy's 20-bar lookback).
const contextLookback = 20

// errFetch is returned (wrapped) when market data can't be fetched. Exported within the
// package for tests.
var errFetch = errors.New("market data fetch failed")

// buildContext snapshots the market situation for one symbol: it fetches recent candles
// (at the config interval), derives the [low, high] channel from the prior contextLookback
// bars (excluding the current one), classifies the regime, and recalls similar past
// episodes from memory. mem may be nil (recall skipped) for tests.
func buildContext(ex exchange.Exchange, symbol, interval string, mem *memory.Store, recallK int) (brain.Context, error) {
	candles, err := ex.GetKlines(symbol, interval, contextLookback+15)
	if err != nil {
		return brain.Context{}, fmt.Errorf("%w: %v", errFetch, err)
	}
	if len(candles) < contextLookback+1 {
		return brain.Context{}, fmt.Errorf("not enough candles for %s: got %d, need %d", symbol, len(candles), contextLookback+1)
	}

	price := candles[len(candles)-1].Close

	// Channel = high/low over the contextLookback bars BEFORE the current one.
	prior := candles[len(candles)-1-contextLookback : len(candles)-1]
	low, high := prior[0].Low, prior[0].High
	for _, c := range prior {
		if c.High > high {
			high = c.High
		}
		if c.Low < low {
			low = c.Low
		}
	}
	regime := classifyRegime(price, low, high)

	ctx := brain.Context{Symbol: symbol, Regime: regime, Price: price}
	if mem != nil {
		ctx.Past = mem.Recall(symbol, regime, recallK)
	}
	return ctx, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestBuildContext -v`
Expected: PASS (3개). gofmt·vet 클린.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add cmd/agent/context.go cmd/agent/context_test.go
git commit -m "feat(cmd/agent): buildContext — 캔들→채널→regime→memory.Recall

stub exchange로 regime분류·price·fetch실패·캔들부족 검증.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: buildAccount (balance 실패·committed risk 합산)

guard가 검사할 계좌 스냅샷. balance 조회 실패 시 BalanceOK=false(진입 차단). 다른 심볼들의 열린 포지션 합산 리스크 계산.

**Files:**
- Create: `cmd/agent/account.go`
- Test: `cmd/agent/account_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/agent/account_test.go`:
```go
package main

import (
	"errors"
	"testing"

	"go-bot/pkg/exchange"
)

func TestBuildAccountBalanceFailureBlocks(t *testing.T) {
	ex := &stubExchange{balanceErr: errors.New("api down"), ticker: 0.5}
	acc, err := buildAccount(ex, "WLDUSDT", []string{"WLDUSDT"}, 3, 10.0)
	if err != nil {
		t.Fatalf("buildAccount should not hard-error on balance failure: %v", err)
	}
	if acc.BalanceOK {
		t.Fatal("BalanceOK should be false when GetBalance fails")
	}
}

func TestBuildAccountSumsCommittedRisk(t *testing.T) {
	// BTC has an open LONG with a stop -> contributes risk. WLD (the entry candidate)
	// must NOT count its own (it has no position here anyway).
	ex := &stubExchange{
		balance: 100, ticker: 0.5,
		positions: map[string]*exchange.Position{
			"BTCUSDT": {Symbol: "BTCUSDT", Side: "LONG", Size: 0.01, EntryPrice: 60000, StopLossPrice: 59000},
		},
	}
	acc, err := buildAccount(ex, "WLDUSDT", []string{"WLDUSDT", "BTCUSDT"}, 3, 10.0)
	if err != nil {
		t.Fatalf("buildAccount: %v", err)
	}
	if !acc.BalanceOK {
		t.Fatal("BalanceOK should be true")
	}
	// risk = size*|entry-sl| = 0.01*1000 = 10
	if acc.CommittedRiskUSDT < 9.99 || acc.CommittedRiskUSDT > 10.01 {
		t.Fatalf("CommittedRiskUSDT = %v, want ~10", acc.CommittedRiskUSDT)
	}
	if acc.Symbol != "WLDUSDT" || acc.Leverage != 3 || acc.MaxPortfolioRisk != 10.0 {
		t.Fatalf("account fields wrong: %+v", acc)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestBuildAccount`
Expected: FAIL — `undefined: buildAccount`.

- [ ] **Step 3: 최소 구현 작성**

`cmd/agent/account.go`:
```go
package main

import (
	"go-bot/pkg/agent"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// buildAccount snapshots the account state the guard checks an entry against. A balance
// lookup failure sets BalanceOK=false (guard blocks entries) rather than erroring the
// whole tick. CommittedRiskUSDT sums the stop-loss risk of OTHER symbols' open positions
// (the candidate symbol's own risk is not pre-committed). leverage and maxPortfolioRisk
// come from config.
func buildAccount(ex exchange.Exchange, symbol string, allSymbols []string, leverage int, maxPortfolioRisk float64) (agent.AccountState, error) {
	acc := agent.AccountState{
		Symbol:           symbol,
		Leverage:         leverage,
		MaxPortfolioRisk: maxPortfolioRisk,
	}

	bal, err := ex.GetBalance()
	if err != nil {
		acc.BalanceOK = false
	} else {
		acc.Balance = bal
		acc.BalanceOK = true
	}

	if price, err := ex.GetTicker(symbol); err == nil {
		acc.Price = price
	}

	// Sum committed risk from other symbols' open positions.
	var committed float64
	for _, sym := range allSymbols {
		if sym == symbol {
			continue
		}
		pos, err := ex.GetPosition(sym)
		if err != nil || pos == nil || pos.Side == "NONE" || pos.Size == 0 {
			continue
		}
		committed += strategy.PositionRiskUSDT(pos.Size, pos.EntryPrice, pos.StopLossPrice)
	}
	acc.CommittedRiskUSDT = committed

	return acc, nil
}
```

(주: `MinOrderQty`는 0으로 둔다 — stub/실거래 공통. guard의 최소수량 규칙은 MinOrderQty>0일 때만 적용되므로 0이면 보수적으로 통과시키되, 실 배선은 main.go에서 instruments-info를 못 가져오면 0 유지. 정밀 최소수량은 2-F 범위.)

- [ ] **Step 4: 테스트 통과 확인**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -run TestBuildAccount -v`
Expected: PASS (2개). gofmt·vet 클린.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add cmd/agent/account.go cmd/agent/account_test.go
git commit -m "feat(cmd/agent): buildAccount — balance실패=BalanceOK false·타심볼 committed risk 합산

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: main.go — 진입점·틱 루프·executor 배선

부품을 엮는다. config·exchange·council·memory·guard·runner 배선 + `time.Ticker` 루프 + 페이퍼 executor(로그·주문0) + OnReject 로그. 컴파일·실행 smoke만 검증(루프 자체는 단위테스트 대상 아님 — 헬퍼는 Task 1~4서 검증됨).

**Files:**
- Create: `cmd/agent/main.go`

- [ ] **Step 1: 구현 작성**

`cmd/agent/main.go`:
```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
	"go-bot/pkg/agent/memory"
	"go-bot/pkg/agent/runner"
	"go-bot/pkg/config"
	"go-bot/pkg/exchange"
)

// agentMinConfidence is the guard's confidence floor for the AI agent. Below this the
// guard downgrades entries to HOLD. Conservative for paper validation.
const agentMinConfidence = 0.55

func main() {
	_ = godotenv.Load() // best-effort: .env for keys (BYBIT_*, DEEPSEEK_*)

	cfg := config.GetConfig()

	ex := exchange.NewBybitExchange(cfg.BybitAPIKey, cfg.BybitAPISecret, false)

	council, label := pickCouncil(os.Getenv)
	mem := memory.New("agent_memory.json")
	g := guard.New(agentMinConfidence)

	r := &runner.Runner{
		Council: council,
		Guard:   g,
		Execute: func(sd agent.SafeDecision) error {
			d := sd.Decision()
			log.Printf("[PAPER ENTRY] %s size=%.2f%% SL=%.2f%% TP=%.2f%% conf=%.2f reason=%q",
				d.Action, d.SizePct, d.StopLossPct, d.TakeProfitPct, d.Confidence, d.Reasoning)
			return nil // paper: no order placed
		},
		Record: func(ep agent.TradeEpisode) error {
			ep.OpenedAt = time.Now()
			return mem.Record(ep)
		},
		OnReject: func(rs []agent.Rejection) {
			for _, rj := range rs {
				log.Printf("[GUARD] %s: %s", rj.Rule, rj.Message)
			}
		},
		NowNano: func() int64 { return time.Now().UnixNano() },
		Nonce:   nonce,
	}

	period := tickInterval(cfg.Interval)
	log.Printf("AI agent starting (PAPER) — council=%s symbols=%v interval=%s(%s) risk=%.1f%% lev=%d",
		label, cfg.Symbols, cfg.Interval, period, cfg.RiskPercentage, cfg.Leverage)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runCycle(r, ex, cfg, mem) // run once immediately, then on the ticker
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("AI agent stopping (signal received)")
			return
		case <-ticker.C:
			runCycle(r, ex, cfg, mem)
		}
	}
}

// runCycle runs one tick across all configured symbols. A per-symbol error is logged and
// skipped so other symbols and later ticks continue (error isolation).
func runCycle(r *runner.Runner, ex exchange.Exchange, cfg *config.Config, mem *memory.Store) {
	for _, sym := range cfg.Symbols {
		bctx, err := buildContext(ex, sym, cfg.Interval, mem, 5)
		if err != nil {
			log.Printf("[%s] context error: %v (skipping)", sym, err)
			continue
		}
		acc, err := buildAccount(ex, sym, cfg.Symbols, cfg.Leverage, cfg.MaxPortfolioRiskPct)
		if err != nil {
			log.Printf("[%s] account error: %v (skipping)", sym, err)
			continue
		}
		// Carry the risk budget the council should request into the decision via guard;
		// the council decides size, the guard clamps. Log the situation for visibility.
		log.Printf("[%s] regime=%s price=%.6f balanceOK=%v committedRisk=%.2f",
			sym, bctx.Regime, bctx.Price, acc.BalanceOK, acc.CommittedRiskUSDT)
		if err := r.RunOnce(bctx, acc); err != nil {
			log.Printf("[%s] cycle error: %v", sym, err)
		}
	}
}

// nonce returns a short random hex string for episode IDs.
func nonce() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// compile-time: ensure council types satisfy the interface (defensive).
var _ brain.Council = (*brain.MockCouncil)(nil)
```

- [ ] **Step 2: 빌드·vet·gofmt 확인**

Run: `cd /Users/mr.joo/Desktop/go && go build ./cmd/agent/ && go vet ./cmd/agent/ && gofmt -l cmd/agent/`
Expected: 빌드 성공, vet 무출력, gofmt 빈 출력.

- [ ] **Step 3: 전체 테스트(회귀 확인)**

Run: `cd /Users/mr.joo/Desktop/go && go test ./cmd/agent/ -v 2>&1 | tail -20`
Expected: 모든 cmd/agent 테스트 PASS.

- [ ] **Step 4: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add cmd/agent/main.go
git commit -m "feat(cmd/agent): main — 틱 루프·페이퍼 executor·OnReject 배선

config 재사용·council 자동선택·심볼별 에러격리·SIGINT 종료.
페이퍼=주문0(진입 의도 로그·episode memory 기록만).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: .gitignore + 통합 검증 + E2E smoke

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: agent_memory.json 무시**

`.gitignore`의 `trades.json` 줄 아래에 추가:
```
agent_memory.json
```

- [ ] **Step 2: 전 패키지 build/vet/race**

Run:
```bash
cd /Users/mr.joo/Desktop/go
go build ./... && go vet ./... && go test -race ./...
```
Expected: build/vet 무출력, 모든 패키지 `ok`. 특히 `pkg/bot`·`pkg/exchange`·`pkg/ai` 회귀 0.

- [ ] **Step 3: E2E smoke (Mock council, 1사이클)**

키 없이 바이너리를 띄워 1사이클(즉시 실행분)이 panic 없이 도는지 확인 후 SIGINT 종료:
```bash
cd /Users/mr.joo/Desktop/go
go build -o /tmp/agent ./cmd/agent
DEEPSEEK_API_KEY= timeout 8 /tmp/agent 2>&1 | head -20 || true
```
Expected: `AI agent starting (PAPER) — council=mock ...` 로그 + 심볼별 `regime=... price=...` 로그(공개 kline 실fetch). MockCouncil이라 HOLD → 진입로그 없음·panic 0. (잔고조회는 키 없으면 실패 가능 → `balanceOK=false` 로그, 정상.)

- [ ] **Step 4: classifyRegime 일치 수동 점검**

Run: `cd /Users/mr.joo/Desktop/go && grep -A12 "func classifyRegime" cmd/agent/regime.go pkg/agent/runner/runner.go`
Expected: 두 구현의 로직(상/하단 10% 기준)이 동일. 다르면 cmd/agent 쪽을 runner에 맞춤.

- [ ] **Step 5: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add .gitignore
git commit -m "chore(cmd/agent): agent_memory.json gitignore + Phase 2-E 통합검증

전 패키지 build/vet/-race green, mock council E2E smoke(공개kline fetch·panic0).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: 문서 갱신 (PROGRESS.md)

**Files:**
- Modify: `PROGRESS.md`

- [ ] **Step 1: Phase 2-E 완료 기록 추가**

`PROGRESS.md` "다음 세션 재개 지점" 최상단(Phase 2-A~D 항목 위)에 추가:
```markdown
### 🤖 [AI 에이전트 Phase 2-E 완료] cmd/agent 페이퍼 가동 — DeepSeek 자동선택, 다음=2-F·회고 (2026-06-30)

**설계서**: `docs/superpowers/specs/2026-06-30-ai-agent-phase2e-design.md`. **계획**: `docs/superpowers/plans/2026-06-30-ai-agent-phase2e.md`. 브랜치 `feat/ai-agent-phase2`.
- ✅ **독립 실행파일 `cmd/agent`**(실거래 봇·웹대시보드 무변경·회귀0): config 재사용(symbols·interval·risk·lev)·exchange 공개데이터·`time.Ticker` 틱 루프·심볼별 에러격리·SIGINT 종료.
- ✅ **council 자동선택**(`pickCouncil`): `DEEPSEEK_API_KEY` 있으면 실 DeepSeek(LLMCouncil+OpenAICompatLLM·`api.deepseek.com/v1`·`deepseek-v4-flash`), 없으면 MockCouncil(HOLD). 라벨 기동로그.
- ✅ **buildContext/buildAccount**: 캔들→채널→`classifyRegime`(runner와 동일기준)→`memory.Recall` / balance실패=BalanceOK false(guard 진입차단)·타심볼 committed risk 합산. stub exchange 단위테스트.
- ✅ **페이퍼 executor**: 진입 의도를 풍부한 로그(방향·size·SL/TP·신뢰도·근거)·**주문0**. episode를 `agent_memory.json`에 기록. OnReject로 guard 차단사유 가시화.
- ✅ **검증**: 전 패키지 build/vet/-race green, mock council E2E smoke(공개kline 실fetch·panic0). DeepSeek 모델/가격 공식문서 확인(flash 입력 $0.0028/1M).
- 🔴 **DeepSeek 실호출 준비됨**(키·잔액 OK). `DEEPSEEK_API_KEY` 주입하고 `cmd/agent` 띄우면 즉시 실 council. 단 첫 실호출은 프롬프트 검증 필요.
- 🔜 **다음 = 2-F**(실 DeepSeek 켜고 결정 품질·프롬프트 튜닝·로그 관찰) → 회고 자동화(포지션 청산 감지→memory.Close)·실거래 주문 집행.
```

- [ ] **Step 2: 커밋**

```bash
cd /Users/mr.joo/Desktop/go
git add PROGRESS.md
git commit -m "docs: AI 에이전트 Phase 2-E(cmd/agent 페이퍼 가동) 완료 기록

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## 완료 기준 (Phase 2-E Definition of Done)

- [ ] `cmd/agent/regime.go`: classifyRegime(runner 일치)·tickInterval, 테스트 통과.
- [ ] `cmd/agent/council.go`: pickCouncil 키 유무 분기, 테스트 통과.
- [ ] `cmd/agent/context.go`: buildContext, stub exchange 테스트(regime·price·fetch실패·캔들부족).
- [ ] `cmd/agent/account.go`: buildAccount, balance실패·committed risk 합산 테스트.
- [ ] `cmd/agent/main.go`: 틱 루프·페이퍼 executor·OnReject 배선, 빌드/vet 클린.
- [ ] `.gitignore`에 agent_memory.json.
- [ ] 전 패키지 build/vet/-race green, 실거래 봇·pkg/ai 무변경 회귀0.
- [ ] mock council E2E smoke(공개kline fetch·panic0).
- [ ] PROGRESS.md 갱신.

**비목표(이 plan 아님)**: 2-F(실 DeepSeek 호출 관찰·프롬프트 튜닝), 회고 자동화(memory.Close 배선), 실거래 주문 집행, 정밀 MinOrderQty(instruments-info) 배선.
