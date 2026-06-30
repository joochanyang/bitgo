package runner

import (
	"errors"
	"strings"
	"testing"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
)

func TestClassifyRegime(t *testing.T) {
	cases := []struct {
		price, low, high float64
		want             string
	}{
		{0.59, 0.50, 0.60, "trending_up"},
		{0.51, 0.50, 0.60, "trending_down"},
		{0.55, 0.50, 0.60, "ranging"},
	}
	for _, c := range cases {
		if got := classifyRegime(c.price, c.low, c.high); got != c.want {
			t.Errorf("classifyRegime(%v,%v,%v)=%s want %s", c.price, c.low, c.high, got, c.want)
		}
	}
}

func TestEpisodeID(t *testing.T) {
	id := episodeID("WLDUSDT", 1700000000123456789, "abc")
	if !strings.HasPrefix(id, "WLDUSDT-") || !strings.HasSuffix(id, "-abc") {
		t.Fatalf("unexpected episode id: %s", id)
	}
	if episodeID("WLDUSDT", 1700000000123456789, "xyz") == id {
		t.Fatal("different nonce should yield different id")
	}
}

func TestRunOnceExecutesAndRecords(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, TakeProfitPct: 4, Confidence: 0.9,
	})
	var executed *agent.SafeDecision
	var recorded *agent.TradeEpisode
	r := &Runner{
		Council: council,
		Guard:   guard.New(0.55),
		Execute: func(sd agent.SafeDecision) error { executed = &sd; return nil },
		Record:  func(ep agent.TradeEpisode) error { recorded = &ep; return nil },
		NowNano: func() int64 { return 42 },
		Nonce:   func() string { return "n1" },
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

// 핵심 안전 불변식: council이 ENTER를 외쳐도 guard가 막으면(저confidence) 무실행·무기록.
// "AI가 폭주해도 돈 안 나간다"를 직접 검증한다.
func TestRunOnceGuardBlockedEntryDoesNotExecute(t *testing.T) {
	// council은 진입을 원하지만 confidence 0.4 < min 0.55 → guard가 HOLD로 강등.
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.4,
	})
	executedCalled, recordedCalled := false, false
	var rejected []agent.Rejection
	r := &Runner{
		Council:  council,
		Guard:    guard.New(0.55),
		Execute:  func(sd agent.SafeDecision) error { executedCalled = true; return nil },
		Record:   func(ep agent.TradeEpisode) error { recordedCalled = true; return nil },
		OnReject: func(rs []agent.Rejection) { rejected = rs },
		NowNano:  func() int64 { return 1 },
		Nonce:    func() string { return "x" },
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	if err := r.RunOnce(brain.Context{Symbol: "WLDUSDT"}, acc); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if executedCalled {
		t.Fatal("guard-blocked entry MUST NOT execute (money safety)")
	}
	if recordedCalled {
		t.Fatal("guard-blocked entry should not record")
	}
	// 사유가 OnReject로 surface 되어야 운영자가 왜 막혔는지 안다.
	if len(rejected) == 0 {
		t.Fatal("OnReject should receive the guard's rejection reasons")
	}
}

// Execute는 성공했는데 Record가 실패하면 ErrOrphanRecord로 구분된다(고아 포지션 인지).
func TestRunOnceOrphanRecordOnRecordFailure(t *testing.T) {
	council := brain.NewMockCouncil(agent.Decision{
		Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.9,
	})
	executed := false
	r := &Runner{
		Council: council,
		Guard:   guard.New(0.55),
		Execute: func(sd agent.SafeDecision) error { executed = true; return nil },
		Record:  func(ep agent.TradeEpisode) error { return errors.New("disk full") },
		NowNano: func() int64 { return 7 },
		Nonce:   func() string { return "z" },
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	err := r.RunOnce(brain.Context{Symbol: "WLDUSDT"}, acc)
	if !executed {
		t.Fatal("order should have executed before record")
	}
	if !errors.Is(err, ErrOrphanRecord) {
		t.Fatalf("record failure after execute must wrap ErrOrphanRecord, got %v", err)
	}
	if !strings.Contains(err.Error(), "WLDUSDT-7-z") {
		t.Fatalf("orphan error should include episode id, got %v", err)
	}
}

// council 에러는 전파되고 무실행.
func TestRunOnceCouncilErrorPropagates(t *testing.T) {
	executed := false
	r := &Runner{
		Council: errCouncil{},
		Guard:   guard.New(0.55),
		Execute: func(sd agent.SafeDecision) error { executed = true; return nil },
		Record:  func(ep agent.TradeEpisode) error { return nil },
		NowNano: func() int64 { return 1 },
		Nonce:   func() string { return "x" },
	}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}

	err := r.RunOnce(brain.Context{Symbol: "WLDUSDT"}, acc)
	if err == nil || !strings.Contains(err.Error(), "council") {
		t.Fatalf("expected council error, got %v", err)
	}
	if executed {
		t.Fatal("council error must not execute")
	}
}

// council이 항상 에러를 내는 테스트용 stub.
type errCouncil struct{}

func (errCouncil) Deliberate(ctx brain.Context) (agent.Decision, error) {
	return agent.Decision{}, errors.New("llm down")
}

// regime 경계: high==low(degenerate)와 정확히 10% 경계.
func TestClassifyRegimeBoundaries(t *testing.T) {
	if got := classifyRegime(0.5, 0.5, 0.5); got != "ranging" {
		t.Fatalf("high==low should be ranging, got %s", got)
	}
	// span=0.10, 상단경계 정확히 0.59(=high-0.1*span): >= 라 trending_up.
	if got := classifyRegime(0.59, 0.50, 0.60); got != "trending_up" {
		t.Fatalf("exact top boundary should be trending_up, got %s", got)
	}
	// 하단경계 정확히 0.51(=low+0.1*span): <= 라 trending_down.
	if got := classifyRegime(0.51, 0.50, 0.60); got != "trending_down" {
		t.Fatalf("exact bottom boundary should be trending_down, got %s", got)
	}
}
