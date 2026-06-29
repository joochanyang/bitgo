package memory

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go-bot/pkg/agent"
)

func tmpStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "episodes.json")
	return New(path)
}

func TestRecordAndAll(t *testing.T) {
	s := tmpStore(t)
	ep := agent.TradeEpisode{
		ID: "e1", OpenedAt: time.Unix(1000, 0), Symbol: "WLDUSDT",
		Regime: "trending_up", EntryPrice: 0.5,
		Decision: agent.Decision{Action: agent.ActionEnterLong, Confidence: 0.7},
	}
	if err := s.Record(ep); err != nil {
		t.Fatalf("Record: %v", err)
	}
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ID != "e1" {
		t.Fatalf("expected 1 episode e1, got %+v", all)
	}
}

func TestAllEmpty(t *testing.T) {
	s := tmpStore(t)
	all, err := s.All()
	if err != nil {
		t.Fatalf("All on empty: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty, got %+v", all)
	}
}

func TestRecallMatchesSymbolAndRegime(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "e1", "WLDUSDT", "trending_up", 100)
	mustRecord(t, s, "e2", "BTCUSDT", "trending_up", 200)
	mustRecord(t, s, "e3", "WLDUSDT", "ranging", 300)
	mustRecord(t, s, "e4", "WLDUSDT", "trending_up", 400)
	got := s.Recall("WLDUSDT", "trending_up", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 matches (e1,e4), got %d: %+v", len(got), got)
	}
	if got[0].ID != "e4" || got[1].ID != "e1" {
		t.Fatalf("expected recency order e4,e1; got %s,%s", got[0].ID, got[1].ID)
	}
}

func TestRecallRespectsLimit(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "a", "WLDUSDT", "ranging", 1)
	mustRecord(t, s, "b", "WLDUSDT", "ranging", 2)
	mustRecord(t, s, "c", "WLDUSDT", "ranging", 3)
	got := s.Recall("WLDUSDT", "ranging", 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 (limit), got %d", len(got))
	}
	if got[0].ID != "c" || got[1].ID != "b" {
		t.Fatalf("expected most-recent c,b; got %s,%s", got[0].ID, got[1].ID)
	}
}

func mustRecord(t *testing.T, s *Store, id, sym, regime string, ts int64) {
	t.Helper()
	if err := s.Record(agent.TradeEpisode{ID: id, Symbol: sym, Regime: regime, OpenedAt: time.Unix(ts, 0)}); err != nil {
		t.Fatalf("Record %s: %v", id, err)
	}
}

func TestCloseFillsOutcome(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "e1", "WLDUSDT", "trending_up", 100)
	if err := s.Close("e1", time.Unix(500, 0), 0.55, 10.0, "tp"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	all, _ := s.All()
	if !all[0].Closed || all[0].PnLPct != 10.0 || all[0].ExitReason != "tp" {
		t.Fatalf("episode not closed correctly: %+v", all[0])
	}
}

func TestStatsWinRate(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "w1", "WLDUSDT", "x", 1)
	mustRecord(t, s, "w2", "WLDUSDT", "x", 2)
	mustRecord(t, s, "l1", "WLDUSDT", "x", 3)
	mustRecord(t, s, "open", "WLDUSDT", "x", 4)
	mustClose(t, s, "w1", 5.0)
	mustClose(t, s, "w2", 3.0)
	mustClose(t, s, "l1", -4.0)
	st := s.Stats()
	if st.Closed != 3 {
		t.Fatalf("expected 3 closed, got %d", st.Closed)
	}
	if st.Wins != 2 {
		t.Fatalf("expected 2 wins, got %d", st.Wins)
	}
	if st.WinRate < 0.66 || st.WinRate > 0.67 {
		t.Fatalf("expected winRate ~0.667, got %v", st.WinRate)
	}
}

func mustClose(t *testing.T, s *Store, id string, pnlPct float64) {
	t.Helper()
	if err := s.Close(id, time.Unix(999, 0), 0, pnlPct, "test"); err != nil {
		t.Fatalf("Close %s: %v", id, err)
	}
}

// 존재하지 않는 id로 Close하면 os.ErrNotExist를 반환한다(errors.Is로 구분 가능).
func TestCloseUnknownID(t *testing.T) {
	s := tmpStore(t)
	mustRecord(t, s, "e1", "WLDUSDT", "x", 1)
	err := s.Close("nope", time.Unix(1, 0), 0, 1.0, "test")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist for unknown id, got %v", err)
	}
}

// 동시 Record가 서로의 에피소드를 덮어쓰지 않는다(mutex가 read-modify-write를 직렬화).
// -race로 돌리면 락이 빠졌을 때 데이터 경합/유실을 잡아낸다.
func TestConcurrentRecordNoLoss(t *testing.T) {
	s := tmpStore(t)
	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "e" + string(rune('A'+i%26)) + string(rune('0'+i/26))
			_ = s.Record(agent.TradeEpisode{ID: id, Symbol: "WLDUSDT", Regime: "x", OpenedAt: time.Unix(int64(i), 0)})
		}(i)
	}
	wg.Wait()
	all, err := s.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != n {
		t.Fatalf("concurrent Record lost episodes: expected %d, got %d", n, len(all))
	}
}
