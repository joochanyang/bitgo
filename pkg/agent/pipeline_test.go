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

	if safe.Action() != agent.ActionHold {
		t.Fatalf("guard must block SL-less entry, got %s", safe.Action())
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
	if safe.Action() != agent.ActionEnterLong {
		t.Fatalf("safe entry should pass, got %s", safe.Action())
	}
}
