package exchange

import (
	"testing"
)

// mkRow builds a raw Bybit kline row [startMs, open, high, low, close, volume, turnover].
func mkRow(ms, price string) []string {
	return []string{ms, price, price, price, price, "1", "1"}
}

func TestMapInterval(t *testing.T) {
	cases := map[string]string{
		"1h":  "60",
		"4h":  "240",
		"30m": "30",
		"15m": "15",
		"5m":  "5",
		"D":   "D", // unknown intervals pass through untouched
		"60":  "60",
	}
	for in, want := range cases {
		if got := mapInterval(in); got != want {
			t.Errorf("mapInterval(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseKlineListChronological(t *testing.T) {
	// Bybit returns newest-first; parseKlineList must emit oldest-first.
	list := [][]string{
		mkRow("3000", "30"),
		mkRow("2000", "20"),
		mkRow("1000", "10"),
	}
	got := parseKlineList(list)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Close != 10 || got[1].Close != 20 || got[2].Close != 30 {
		t.Errorf("not chronological: %v %v %v", got[0].Close, got[1].Close, got[2].Close)
	}
	if got[0].Time.UnixMilli() != 1000 {
		t.Errorf("first time ms = %d, want 1000", got[0].Time.UnixMilli())
	}
}

func TestParseKlineListSkipsMalformed(t *testing.T) {
	list := [][]string{
		mkRow("2000", "20"),
		{"1500"}, // malformed (<6 fields) -> skipped
		mkRow("1000", "10"),
	}
	got := parseKlineList(list)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (malformed row skipped)", len(got))
	}
}

func TestMergeKlinePagesDedupeAndCap(t *testing.T) {
	// Two pages with an overlapping ms=3000 row; merge must dedupe and stay chronological.
	pageA := parseKlineList([][]string{mkRow("3000", "30"), mkRow("2000", "20"), mkRow("1000", "10")})
	pageB := parseKlineList([][]string{mkRow("5000", "50"), mkRow("4000", "40"), mkRow("3000", "30")})

	got := mergeKlinePages([][]Candle{pageA, pageB}, 0) // 0 total = keep all
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (ms=3000 deduped)", len(got))
	}
	wantMs := []int64{1000, 2000, 3000, 4000, 5000}
	for i, ms := range wantMs {
		if got[i].Time.UnixMilli() != ms {
			t.Errorf("got[%d].ms = %d, want %d", i, got[i].Time.UnixMilli(), ms)
		}
	}
}

func TestMergeKlinePagesKeepsMostRecent(t *testing.T) {
	// When total caps the result, the MOST RECENT candles must be kept (tail of chronological order).
	page := parseKlineList([][]string{
		mkRow("5000", "50"), mkRow("4000", "40"), mkRow("3000", "30"),
		mkRow("2000", "20"), mkRow("1000", "10"),
	})
	got := mergeKlinePages([][]Candle{page}, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantMs := []int64{3000, 4000, 5000}
	for i, ms := range wantMs {
		if got[i].Time.UnixMilli() != ms {
			t.Errorf("got[%d].ms = %d, want %d (most-recent kept)", i, got[i].Time.UnixMilli(), ms)
		}
	}
}

func TestMergeKlinePagesZeroTotalReturnsAll(t *testing.T) {
	page := parseKlineList([][]string{mkRow("2000", "20"), mkRow("1000", "10")})
	got := mergeKlinePages([][]Candle{page}, 0)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}
