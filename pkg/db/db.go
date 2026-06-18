package db

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Trade represents a record of a completed or placed trade
type Trade struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Side        string    `json:"side"` // "LONG", "SHORT"
	Size        float64   `json:"size"`
	EntryPrice  float64   `json:"entry_price"`
	ExitPrice   float64   `json:"exit_price,omitempty"`
	RealizedPnL float64   `json:"realized_pnl"`
	Leverage    int       `json:"leverage"`
	Timestamp   time.Time `json:"timestamp"`
	IsPaper     bool      `json:"is_paper"`
	Status      string    `json:"status"` // "OPEN", "CLOSED"
}

// LogEntry represents a single system log
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // "INFO", "WARN", "ERROR"
	Message   string `json:"message"`
}

var (
	tradeFile = "trades.json"
	logFile   = "logs.json"
	dbMu      sync.RWMutex
	maxLogs   = 200 // Cap logs to prevent files growing too large
)

// writeFileAtomic writes data to a temp file in the same directory, then renames it
// over the target. rename(2) is atomic on the same filesystem, so a crash or a concurrent
// reader never observes a half-written (truncated) JSON ledger. Mode 0600 keeps records
// owner-readable only.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail out before the rename succeeds.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// AddTrade appends a trade record to trades.json
func AddTrade(t Trade) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	trades, err := loadTradesRaw()
	if err != nil {
		trades = []Trade{}
	}

	trades = append(trades, t)

	data, err := json.MarshalIndent(trades, "", "  ")
	if err != nil {
		return err
	}

	return writeFileAtomic(tradeFile, data)
}

// UpdateTrade updates an existing trade (e.g., when closing a position)
func UpdateTrade(updated Trade) error {
	dbMu.Lock()
	defer dbMu.Unlock()

	trades, err := loadTradesRaw()
	if err != nil {
		return err
	}

	// 1. Prefer an exact ID match (used by syncTradeHistory, which carries the real ID).
	idx := -1
	for i, t := range trades {
		if t.ID == updated.ID {
			idx = i
			break
		}
	}

	// 2. Fall back to the symbol's OPEN record when closing (the engine's CLOSE path
	//    generates a fresh ID, so it cannot match by ID). Pick the LAST open record for
	//    the symbol to stay deterministic if more than one somehow exists.
	if idx == -1 && updated.Status == "CLOSED" {
		for i, t := range trades {
			if t.Symbol == updated.Symbol && t.Status == "OPEN" {
				idx = i
			}
		}
	}

	// 3. No match: do NOT append a phantom record (that would pollute realized-PnL totals).
	if idx == -1 {
		return fmt.Errorf("UpdateTrade: no matching trade for id=%s symbol=%s status=%s", updated.ID, updated.Symbol, updated.Status)
	}

	// Preserve the original record's ID so the OPEN->CLOSED transition stays one row.
	updated.ID = trades[idx].ID
	trades[idx] = updated

	data, err := json.MarshalIndent(trades, "", "  ")
	if err != nil {
		return err
	}

	return writeFileAtomic(tradeFile, data)
}

// GetTrades returns all trade records
func GetTrades() ([]Trade, error) {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return loadTradesRaw()
}

func loadTradesRaw() ([]Trade, error) {
	if _, err := os.Stat(tradeFile); os.IsNotExist(err) {
		return []Trade{}, nil
	}

	data, err := ioutil.ReadFile(tradeFile)
	if err != nil {
		return nil, err
	}

	var trades []Trade
	if err := json.Unmarshal(data, &trades); err != nil {
		return nil, err
	}

	return trades, nil
}

// AddLog logs a message to console and saves to logs.json
func AddLog(level, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Console output
	fmt.Printf("[%s] [%s] %s\n", timestamp, level, msg)

	dbMu.Lock()
	defer dbMu.Unlock()

	logs, err := loadLogsRaw()
	if err != nil {
		logs = []LogEntry{}
	}

	logs = append(logs, LogEntry{
		Timestamp: timestamp,
		Level:     level,
		Message:   msg,
	})

	// Cap logs
	if len(logs) > maxLogs {
		logs = logs[len(logs)-maxLogs:]
	}

	data, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return
	}

	_ = writeFileAtomic(logFile, data)
}

// GetLogs returns all system logs
func GetLogs() ([]LogEntry, error) {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return loadLogsRaw()
}

func loadLogsRaw() ([]LogEntry, error) {
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return []LogEntry{}, nil
	}

	data, err := ioutil.ReadFile(logFile)
	if err != nil {
		return nil, err
	}

	var logs []LogEntry
	if err := json.Unmarshal(data, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

// LogInfo helper
func LogInfo(format string, v ...interface{}) {
	AddLog("INFO", format, v...)
}

// LogWarn helper
func LogWarn(format string, v ...interface{}) {
	AddLog("WARN", format, v...)
}

// LogError helper
func LogError(format string, v ...interface{}) {
	AddLog("ERROR", format, v...)
}
