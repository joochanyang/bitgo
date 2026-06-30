package runner

import (
	"strings"
	"testing"
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
