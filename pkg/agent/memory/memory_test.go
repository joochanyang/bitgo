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
