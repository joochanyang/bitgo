package memory

import (
	"path/filepath"
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
