package config

import (
	"path/filepath"
	"sync"
	"testing"
)

// setupTempConfig points the package at a temp file and seeds the singleton,
// so the test does not touch the real config.json.
func setupTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	mu.Lock()
	filePath = filepath.Join(dir, "config.json")
	instance = &Config{
		OpenAIModel:    "gpt-4o",
		GeminiModel:    "gemini-1.5-pro",
		AIProvider:     "gemini",
		Symbols:        []string{"WLDUSDT", "FETUSDT"},
		Interval:       "1h",
		Leverage:       3,
		RiskPercentage: 5.0,
		IsPaperTrading: true,
		ServerPort:     "8080",
		ActiveStrategy: "trend_following",
	}
	mu.Unlock()
}

// TestConcurrentGetAndUpdate is the P1-1 regression guard: run with `go test -race`.
// Before the fix, GetConfig returned the shared pointer while UpdateConfig mutated its
// fields in place -> the race detector fired. Now GetConfig returns a snapshot and
// UpdateConfig swaps the pointer under the lock, so this must be clean.
func TestConcurrentGetAndUpdate(t *testing.T) {
	setupTempConfig(t)

	var readers sync.WaitGroup
	var writers sync.WaitGroup
	stop := make(chan struct{})

	// Readers spin until stopped.
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					c := GetConfig()
					_ = c.IsPaperTrading
					_ = c.AIProvider
					if len(c.Symbols) > 0 {
						_ = c.Symbols[0]
					}
				}
			}
		}()
	}

	// Writers do a fixed amount of work, then finish.
	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func(n int) {
			defer writers.Done()
			for j := 0; j < 200; j++ {
				nc := *GetConfig()
				nc.Leverage = (n + j) % 5
				nc.Symbols = []string{"WLDUSDT", "ARBUSDT"}
				if err := UpdateConfig(&nc); err != nil {
					t.Errorf("UpdateConfig failed: %v", err)
					return
				}
				if err := SetPaperTrading(j%2 == 0); err != nil {
					t.Errorf("SetPaperTrading failed: %v", err)
					return
				}
			}
		}(i)
	}

	writers.Wait()
	close(stop)
	readers.Wait()
}

// TestUpdateConfigPreservesModelsAndSnapshot verifies the P0-4 protection still holds
// after the pointer-swap rewrite, and that a returned snapshot is independent.
func TestUpdateConfigPreservesModelsAndSnapshot(t *testing.T) {
	setupTempConfig(t)

	// A UI save with empty model fields must NOT wipe the stored models.
	nc := *GetConfig()
	nc.OpenAIModel = ""
	nc.GeminiModel = ""
	nc.Leverage = 10
	if err := UpdateConfig(&nc); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got := GetConfig()
	if got.OpenAIModel != "gpt-4o" || got.GeminiModel != "gemini-1.5-pro" {
		t.Errorf("models were wiped: openai=%q gemini=%q", got.OpenAIModel, got.GeminiModel)
	}
	if got.Leverage != 10 {
		t.Errorf("leverage not updated: got %d", got.Leverage)
	}

	// Mutating a returned snapshot must not affect the stored config.
	snap := GetConfig()
	snap.Symbols[0] = "MUTATED"
	snap.Leverage = 999
	if again := GetConfig(); again.Symbols[0] == "MUTATED" || again.Leverage == 999 {
		t.Errorf("snapshot is not isolated from stored config")
	}
}

// TestSanitizeRiskParams locks the risk-parameter clamping (notional-explosion guard
// at the config layer): a 999% risk typo and a 0 leverage must be brought into range.
func TestSanitizeRiskParams(t *testing.T) {
	cases := []struct {
		name     string
		inRisk   float64
		inLev    int
		wantRisk float64
		wantLev  int
	}{
		{"risk typo 999 clamps to max", 999, 3, maxRiskPercentage, 3},
		{"negative risk -> default", -1, 3, 5.0, 3},
		{"zero risk -> default", 0, 3, 5.0, 3},
		{"zero leverage -> 1", 5, 0, 5.0, 1},
		{"negative leverage -> 1", 5, -4, 5.0, 1},
		{"leverage over max clamps", 5, 999, 5.0, maxLeverage},
		{"in-range unchanged", 5, 3, 5.0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{RiskPercentage: tc.inRisk, Leverage: tc.inLev}
			sanitizeRiskParams(c)
			if c.RiskPercentage != tc.wantRisk {
				t.Errorf("RiskPercentage = %v, want %v", c.RiskPercentage, tc.wantRisk)
			}
			if c.Leverage != tc.wantLev {
				t.Errorf("Leverage = %v, want %v", c.Leverage, tc.wantLev)
			}
		})
	}
}

// TestSanitizeMaxPortfolioRiskPct locks the portfolio-risk-cap clamping.
func TestSanitizeMaxPortfolioRiskPct(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"zero -> default 10", 0, 10.0},
		{"negative -> default 10", -5, 10.0},
		{"over 100 clamps", 250, maxPortfolioRiskCeil},
		{"in-range unchanged", 15, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{RiskPercentage: 5, Leverage: 3, MaxPortfolioRiskPct: tc.in}
			sanitizeRiskParams(c)
			if c.MaxPortfolioRiskPct != tc.want {
				t.Errorf("MaxPortfolioRiskPct = %v, want %v", c.MaxPortfolioRiskPct, tc.want)
			}
		})
	}
}

// TestUpdateConfigClampsRiskParams: a UI save with a dangerous risk value is sanitized.
func TestUpdateConfigClampsRiskParams(t *testing.T) {
	setupTempConfig(t)
	bad := *GetConfig()
	bad.RiskPercentage = 999
	bad.Leverage = 0
	if err := UpdateConfig(&bad); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got := GetConfig()
	if got.RiskPercentage != maxRiskPercentage {
		t.Errorf("RiskPercentage = %v, want clamped %v", got.RiskPercentage, maxRiskPercentage)
	}
	if got.Leverage != 1 {
		t.Errorf("Leverage = %v, want clamped 1", got.Leverage)
	}
}
