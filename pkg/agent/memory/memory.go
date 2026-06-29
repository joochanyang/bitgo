// Package memory is the agent's long-term memory: it persists every trade episode
// (situation, decision, and — after close — outcome) so the council can recall similar
// past trades and learn from results. Backed by an atomic JSON file, mirroring pkg/db.
package memory

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"go-bot/pkg/agent"
)

// Store is a file-backed episode store. Construct with New.
type Store struct {
	path string
}

// New returns a Store persisting to path. The file is created on first Record.
func New(path string) *Store {
	return &Store{path: path}
}

// All returns every recorded episode, oldest first. An absent file is not an error
// (returns empty), matching pkg/db's loadTradesRaw behaviour.
func (s *Store) All() ([]agent.TradeEpisode, error) {
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		return []agent.TradeEpisode{}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []agent.TradeEpisode{}, nil
	}
	var eps []agent.TradeEpisode
	if err := json.Unmarshal(data, &eps); err != nil {
		return nil, err
	}
	return eps, nil
}

// Record appends an episode and persists atomically (write temp + rename), the same
// crash-safe pattern pkg/db uses for trades.json.
func (s *Store) Record(ep agent.TradeEpisode) error {
	eps, err := s.All()
	if err != nil {
		return err
	}
	eps = append(eps, ep)
	return s.writeAll(eps)
}

// Recall returns up to k past episodes matching the given symbol and regime, most
// recent first. Phase 1 uses simple rule-based matching; embedding similarity is a
// later enhancement. On read error it returns nil (caller treats memory as empty).
func (s *Store) Recall(symbol, regime string, k int) []agent.TradeEpisode {
	all, err := s.All()
	if err != nil {
		return nil
	}
	var matches []agent.TradeEpisode
	for _, ep := range all {
		if ep.Symbol == symbol && ep.Regime == regime {
			matches = append(matches, ep)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].OpenedAt.After(matches[j].OpenedAt)
	})
	if k > 0 && len(matches) > k {
		matches = matches[:k]
	}
	return matches
}

// PerformanceSummary is the aggregate over closed episodes, used by the performance
// tracker to decide when to suggest a stage upgrade (paper -> live -> larger size).
type PerformanceSummary struct {
	Closed  int     `json:"closed"`
	Wins    int     `json:"wins"`
	WinRate float64 `json:"win_rate"`
	AvgPnL  float64 `json:"avg_pnl"`
}

// Close fills in the retrospective outcome for the episode with the given id and
// persists. This is how a decision gets labelled right/wrong after the fact, so future
// Recalls carry the lesson. Returns an error if the id is not found.
func (s *Store) Close(id string, closedAt time.Time, exitPrice, pnlPct float64, reason string) error {
	eps, err := s.All()
	if err != nil {
		return err
	}
	found := false
	for i := range eps {
		if eps[i].ID == id {
			eps[i].Closed = true
			eps[i].ClosedAt = closedAt
			eps[i].ExitPrice = exitPrice
			eps[i].PnLPct = pnlPct
			eps[i].ExitReason = reason
			found = true
			break
		}
	}
	if !found {
		return os.ErrNotExist
	}
	return s.writeAll(eps)
}

// Stats aggregates closed episodes. Open episodes are ignored. On read error it
// returns a zero summary.
func (s *Store) Stats() PerformanceSummary {
	all, err := s.All()
	if err != nil {
		return PerformanceSummary{}
	}
	var sum PerformanceSummary
	var pnlTotal float64
	for _, ep := range all {
		if !ep.Closed {
			continue
		}
		sum.Closed++
		pnlTotal += ep.PnLPct
		if ep.PnLPct > 0 {
			sum.Wins++
		}
	}
	if sum.Closed > 0 {
		sum.WinRate = float64(sum.Wins) / float64(sum.Closed)
		sum.AvgPnL = pnlTotal / float64(sum.Closed)
	}
	return sum
}

func (s *Store) writeAll(eps []agent.TradeEpisode) error {
	data, err := json.MarshalIndent(eps, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
