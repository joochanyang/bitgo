package config

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sync"
)

// Config holds all the bot configuration parameters
type Config struct {
	BybitAPIKey         string   `json:"bybit_api_key"`
	BybitAPISecret      string   `json:"bybit_api_secret"`
	OpenAIAPIKey        string   `json:"openai_api_key"`
	OpenAIModel         string   `json:"openai_model"`
	GeminiAPIKey        string   `json:"gemini_api_key"`
	GeminiModel         string   `json:"gemini_model"`
	AIProvider          string   `json:"ai_provider"` // "openai" or "gemini"
	Symbols             []string `json:"symbols"`     // e.g. ["WLDUSDT", "FETUSDT"]
	Interval            string   `json:"interval"`    // e.g. "15m", "1h", "4h"
	Leverage            int      `json:"leverage"`
	RiskPercentage      float64  `json:"risk_percentage"`        // e.g. 5.0 for 5% of balance per trade
	MaxPortfolioRiskPct float64  `json:"max_portfolio_risk_pct"` // cap on TOTAL risk across all open positions
	IsPaperTrading      bool     `json:"is_paper_trading"`
	ServerPort          string   `json:"server_port"`
	ActiveStrategy      string   `json:"active_strategy"`
}

var (
	instance *Config
	once     sync.Once
	mu       sync.RWMutex
	filePath = "config.json"
)

// GetConfig returns an immutable snapshot of the current config.
// A copy is returned (not the shared pointer) so callers can read fields without
// locking and are never exposed to a concurrent in-place mutation (data race).
func GetConfig() *Config {
	once.Do(func() {
		instance = loadConfig()
	})
	mu.RLock()
	defer mu.RUnlock()
	snapshot := *instance
	// Copy the slice too, so a later UpdateConfig replacing instance.Symbols
	// cannot be observed mid-write through a shared backing array.
	if instance.Symbols != nil {
		snapshot.Symbols = append([]string(nil), instance.Symbols...)
	}
	return &snapshot
}

// loadConfig loads the configuration from file or returns defaults
func loadConfig() *Config {
	var conf Config
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		// Return default configuration
		return &Config{
			BybitAPIKey:         os.Getenv("BYBIT_API_KEY"),
			BybitAPISecret:      os.Getenv("BYBIT_API_SECRET"),
			OpenAIAPIKey:        os.Getenv("OPENAI_API_KEY"),
			OpenAIModel:         "gpt-4o",
			GeminiAPIKey:        os.Getenv("GEMINI_API_KEY"),
			GeminiModel:         "gemini-1.5-pro",
			AIProvider:          "gemini", // Default to gemini
			Symbols:             []string{"WLDUSDT", "FETUSDT", "NEARUSDT", "RENDERUSDT"},
			Interval:            "1h",
			Leverage:            3,
			RiskPercentage:      5.0,
			MaxPortfolioRiskPct: 10.0,
			IsPaperTrading:      true, // Default to paper trading for safety
			ServerPort:          "8080",
			ActiveStrategy:      "trend_following",
		}
	}

	if err := json.Unmarshal(data, &conf); err != nil {
		logError("Failed to parse config.json, using defaults: " + err.Error())
		return &Config{
			IsPaperTrading:      true,
			AIProvider:          "gemini",
			Symbols:             []string{"WLDUSDT", "FETUSDT", "NEARUSDT", "RENDERUSDT"},
			Interval:            "1h",
			Leverage:            3,
			RiskPercentage:      5.0,
			MaxPortfolioRiskPct: 10.0,
			ServerPort:          "8080",
			ActiveStrategy:      "trend_following",
		}
	}

	// Override with environment variables if present
	if envKey := os.Getenv("BYBIT_API_KEY"); envKey != "" {
		conf.BybitAPIKey = envKey
	}
	if envSecret := os.Getenv("BYBIT_API_SECRET"); envSecret != "" {
		conf.BybitAPISecret = envSecret
	}
	if envOpenAI := os.Getenv("OPENAI_API_KEY"); envOpenAI != "" {
		conf.OpenAIAPIKey = envOpenAI
	}
	if envGemini := os.Getenv("GEMINI_API_KEY"); envGemini != "" {
		conf.GeminiAPIKey = envGemini
	}

	sanitizeRiskParams(&conf)
	return &conf
}

// Risk-parameter bounds. RiskPercentage and Leverage feed position sizing directly,
// so out-of-range values (a 999% risk typo, a 0 leverage) must never reach the engine.
const (
	maxRiskPercentage    = 20.0  // a single trade should never risk more than 20% of balance
	maxLeverage          = 20    // hard ceiling on leverage
	maxPortfolioRiskCeil = 100.0 // total portfolio risk can't exceed the whole account
)

// sanitizeRiskParams clamps the sizing-critical fields to safe ranges in place.
func sanitizeRiskParams(c *Config) {
	if c.RiskPercentage <= 0 {
		c.RiskPercentage = 5.0 // default
	} else if c.RiskPercentage > maxRiskPercentage {
		c.RiskPercentage = maxRiskPercentage
	}
	if c.Leverage < 1 {
		c.Leverage = 1
	} else if c.Leverage > maxLeverage {
		c.Leverage = maxLeverage
	}
	if c.MaxPortfolioRiskPct <= 0 {
		c.MaxPortfolioRiskPct = 10.0 // default
	} else if c.MaxPortfolioRiskPct > maxPortfolioRiskCeil {
		c.MaxPortfolioRiskPct = maxPortfolioRiskCeil
	}
}

// UpdateConfig replaces the configuration and saves it to file.
// It builds a new Config and swaps the singleton pointer under the write lock
// (rather than mutating fields in place), so concurrent GetConfig snapshots are
// never torn. Empty model names are treated as "unchanged" so a UI save cannot wipe them.
func UpdateConfig(newConf *Config) error {
	mu.Lock()
	defer mu.Unlock()

	merged := *newConf
	if newConf.OpenAIModel == "" {
		merged.OpenAIModel = instance.OpenAIModel
	}
	if newConf.GeminiModel == "" {
		merged.GeminiModel = instance.GeminiModel
	}
	if newConf.Symbols != nil {
		merged.Symbols = append([]string(nil), newConf.Symbols...)
	}
	sanitizeRiskParams(&merged)

	data, err := json.MarshalIndent(&merged, "", "  ")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filePath, data, 0600); err != nil {
		return err
	}

	instance = &merged
	return nil
}

// SetPaperTrading flips just the paper-trading flag and persists it, under the
// same lock/pointer-swap discipline as UpdateConfig (avoids a lock-free field write).
func SetPaperTrading(isPaper bool) error {
	mu.Lock()
	defer mu.Unlock()

	merged := *instance
	merged.IsPaperTrading = isPaper
	merged.Symbols = append([]string(nil), instance.Symbols...)

	data, err := json.MarshalIndent(&merged, "", "  ")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filePath, data, 0600); err != nil {
		return err
	}

	instance = &merged
	return nil
}

func logError(msg string) {
	// Simple stderr printing, real logger is implemented in db/logs package
	println("[CONFIG ERROR] " + msg)
}
