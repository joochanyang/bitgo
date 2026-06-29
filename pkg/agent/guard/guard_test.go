package guard

import (
	"testing"

	"go-bot/pkg/agent"
)

func TestValidateDowngradesLowConfidence(t *testing.T) {
	g := New(0.55)
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

func TestValidateKeepsConfidentEntry(t *testing.T) {
	g := New(0.55)
	d := agent.Decision{Action: agent.ActionEnterLong, SizePct: 1, StopLossPct: 2, Confidence: 0.80}
	acc := agent.AccountState{Symbol: "WLDUSDT", Balance: 50, Price: 0.5, MinOrderQty: 1, Leverage: 3, MaxPortfolioRisk: 10, BalanceOK: true}
	safe, _ := g.Validate(d, acc)
	if safe.Action != agent.ActionEnterLong {
		t.Fatalf("confident entry should be kept, got %s", safe.Action)
	}
}
