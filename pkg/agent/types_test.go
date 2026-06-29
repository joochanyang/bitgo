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
