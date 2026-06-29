// Package memory is the agent's long-term memory: it persists every trade episode
// (situation, decision, and — after close — outcome) so the council can recall similar
// past trades and learn from results. Backed by an atomic JSON file, mirroring pkg/db.
package memory

import (
	"encoding/json"
	"os"

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
