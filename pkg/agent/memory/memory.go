// Package memory is the agent's long-term memory: it persists every trade episode
// (situation, decision, and — after close — outcome) so the council can recall similar
// past trades and learn from results. Backed by an atomic JSON file, mirroring pkg/db.
package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go-bot/pkg/agent"
)

// Store is a file-backed episode store. Construct with New. All exported methods are
// goroutine-safe: a mutex serializes the read-modify-write of Record/Close so concurrent
// callers cannot clobber each other's episodes (the same guarantee pkg/db gives trades).
type Store struct {
	mu   sync.Mutex
	path string
}

// New returns a Store persisting to path. The file is created on first Record.
func New(path string) *Store {
	return &Store{path: path}
}

// All returns every recorded episode, oldest first. An absent file is not an error
// (returns empty), matching pkg/db's loadTradesRaw behaviour.
func (s *Store) All() ([]agent.TradeEpisode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// loadLocked reads and parses the episode file. Callers must hold s.mu. It exists so
// the read-modify-write methods (Record/Close) can load without re-acquiring the lock
// (which would deadlock).
func (s *Store) loadLocked() ([]agent.TradeEpisode, error) {
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
// crash-safe pattern pkg/db uses for trades.json. The whole read-modify-write is held
// under the lock so a concurrent Record/Close cannot overwrite this episode.
func (s *Store) Record(ep agent.TradeEpisode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	eps, err := s.loadLocked()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	eps, err := s.loadLocked()
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

// writeAll persists episodes atomically: write to a unique temp file in the same dir,
// then rename over the target. Callers must hold s.mu. Mirrors pkg/db.writeFileAtomic —
// unique temp name (no collision), defer-remove on failure (no .tmp litter), and 0600
// perms (trade records are owner-only, not world-readable).
func (s *Store) writeAll(eps []agent.TradeEpisode) error {
	data, err := json.MarshalIndent(eps, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".episodes-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // best-effort cleanup if rename fails or we error out
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}
