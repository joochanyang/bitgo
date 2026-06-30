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
