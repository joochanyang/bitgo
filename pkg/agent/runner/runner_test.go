package runner

import (
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
