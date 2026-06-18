package db

import (
	"path/filepath"
	"testing"
)

// useTempLedger points the package at temp files so tests don't touch real ledgers.
func useTempLedger(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbMu.Lock()
	tradeFile = filepath.Join(dir, "trades.json")
	logFile = filepath.Join(dir, "logs.json")
	dbMu.Unlock()
}

// TestUpdateTradeMatchesByID: a close carrying the real ID updates that row in place.
func TestUpdateTradeByID(t *testing.T) {
	useTempLedger(t)
	if err := AddTrade(Trade{ID: "o1", Symbol: "WLDUSDT", Side: "LONG", Status: "OPEN"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTrade(Trade{ID: "o1", Symbol: "WLDUSDT", Side: "LONG", Status: "CLOSED", RealizedPnL: 5}); err != nil {
		t.Fatalf("UpdateTrade by ID failed: %v", err)
	}
	trades, _ := GetTrades()
	if len(trades) != 1 || trades[0].Status != "CLOSED" || trades[0].RealizedPnL != 5 {
		t.Errorf("expected single CLOSED row with pnl 5, got %+v", trades)
	}
}

// TestUpdateTradeSymbolFallbackPreservesID: the engine's CLOSE path uses a fresh ID;
// it must still match the symbol's OPEN row AND keep the original ID (one row, not two).
func TestUpdateTradeSymbolFallbackPreservesID(t *testing.T) {
	useTempLedger(t)
	_ = AddTrade(Trade{ID: "orig-open", Symbol: "FETUSDT", Side: "LONG", Status: "OPEN"})

	// Close with a different (engine-generated) ID.
	if err := UpdateTrade(Trade{ID: "trade-999", Symbol: "FETUSDT", Side: "LONG", Status: "CLOSED", RealizedPnL: -2}); err != nil {
		t.Fatalf("symbol-fallback close failed: %v", err)
	}

	trades, _ := GetTrades()
	if len(trades) != 1 {
		t.Fatalf("expected 1 row (no phantom append), got %d: %+v", len(trades), trades)
	}
	if trades[0].ID != "orig-open" {
		t.Errorf("expected original ID preserved, got %q", trades[0].ID)
	}
	if trades[0].Status != "CLOSED" {
		t.Errorf("expected CLOSED, got %q", trades[0].Status)
	}
}

// TestUpdateTradeNoMatchErrors: an unknown close must error, NOT append a phantom record.
func TestUpdateTradeNoMatchErrors(t *testing.T) {
	useTempLedger(t)
	_ = AddTrade(Trade{ID: "open-A", Symbol: "WLDUSDT", Side: "LONG", Status: "OPEN"})

	err := UpdateTrade(Trade{ID: "ghost", Symbol: "NEARUSDT", Side: "LONG", Status: "CLOSED"})
	if err == nil {
		t.Error("expected error for unknown close, got nil")
	}
	trades, _ := GetTrades()
	if len(trades) != 1 {
		t.Errorf("expected no phantom row appended, got %d: %+v", len(trades), trades)
	}
}

// TestUpdateTradeMultipleOpenSameSymbolIsDeterministic: with two OPEN rows for one symbol,
// the fallback closes the most recent one deterministically (no arbitrary first-match).
func TestUpdateTradeMultipleOpenSameSymbol(t *testing.T) {
	useTempLedger(t)
	_ = AddTrade(Trade{ID: "first", Symbol: "WLDUSDT", Side: "LONG", Status: "OPEN"})
	_ = AddTrade(Trade{ID: "second", Symbol: "WLDUSDT", Side: "LONG", Status: "OPEN"})

	if err := UpdateTrade(Trade{ID: "close-x", Symbol: "WLDUSDT", Side: "LONG", Status: "CLOSED"}); err != nil {
		t.Fatal(err)
	}
	trades, _ := GetTrades()
	if len(trades) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(trades))
	}
	// The LAST open ("second") should be the one closed.
	var firstStatus, secondStatus string
	for _, tr := range trades {
		if tr.ID == "first" {
			firstStatus = tr.Status
		}
		if tr.ID == "second" {
			secondStatus = tr.Status
		}
	}
	if secondStatus != "CLOSED" || firstStatus != "OPEN" {
		t.Errorf("expected the last OPEN closed: first=%s second=%s", firstStatus, secondStatus)
	}
}
