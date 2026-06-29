// Package memory is the agent's long-term memory: it persists every trade episode
// (situation, decision, and — after close — outcome) so the council can recall similar
// past trades and learn from results. Backed by an atomic JSON file, mirroring pkg/db.
package memory

import (
	"encoding/json"
	"os"
	"sort"

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
