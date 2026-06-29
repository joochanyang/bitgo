package brain

import (
	"testing"

	"go-bot/pkg/agent"
)

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

func TestMockCouncilSatisfiesInterface(t *testing.T) {
	var _ Council = (*MockCouncil)(nil)
}
