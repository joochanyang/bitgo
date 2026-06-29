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

func TestValidateRejectsEntryWithoutStopLoss(t *testing.T) {
	g := New(0.0)
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

func hasRule(rs []agent.Rejection, rule string) bool {
	for _, r := range rs {
		if r.Rule == rule {
			return true
		}
	}
	return false
}
