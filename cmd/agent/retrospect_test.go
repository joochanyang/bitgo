package main

import (
	"testing"

	"go-bot/pkg/agent"
)

func longEpisode() agent.TradeEpisode {
	return agent.TradeEpisode{
		ID: "WLD-1", Symbol: "WLDUSDT", EntryPrice: 0.50,
		Decision: agent.Decision{Action: agent.ActionEnterLong, StopLossPct: 2, TakeProfitPct: 4},
	}
}

func shortEpisode() agent.TradeEpisode {
	return agent.TradeEpisode{
		ID: "WLD-2", Symbol: "WLDUSDT", EntryPrice: 0.50,
		Decision: agent.Decision{Action: agent.ActionEnterShort, StopLossPct: 2, TakeProfitPct: 4},
	}
}

func TestEvalCloseLongTakeProfit(t *testing.T) {
	// long TP at 0.50*1.04 = 0.52; price at/above -> tp, pnl +4%.
	closed, exit, pnl, reason := evalClose(longEpisode(), 0.52)
	if !closed || reason != "tp" {
		t.Fatalf("expected tp close, got closed=%v reason=%q", closed, reason)
	}
	if exit != 0.52 {
		t.Fatalf("exit = %v, want 0.52", exit)
	}
	if pnl < 3.99 || pnl > 4.01 {
		t.Fatalf("pnl = %v, want ~+4", pnl)
	}
}

func TestEvalCloseLongStopLoss(t *testing.T) {
	// long SL at 0.50*0.98 = 0.49; price at/below -> sl, pnl -2%.
	closed, exit, pnl, reason := evalClose(longEpisode(), 0.49)
	if !closed || reason != "sl" {
		t.Fatalf("expected sl close, got closed=%v reason=%q", closed, reason)
	}
	if exit != 0.49 {
		t.Fatalf("exit = %v, want 0.49", exit)
	}
	if pnl < -2.01 || pnl > -1.99 {
		t.Fatalf("pnl = %v, want ~-2", pnl)
	}
}

func TestEvalCloseShortTakeProfit(t *testing.T) {
	// short TP at 0.50*0.96 = 0.48; price at/below -> tp, pnl +4%.
	closed, _, pnl, reason := evalClose(shortEpisode(), 0.48)
	if !closed || reason != "tp" {
		t.Fatalf("expected tp close, got closed=%v reason=%q", closed, reason)
	}
	if pnl < 3.99 || pnl > 4.01 {
		t.Fatalf("pnl = %v, want ~+4 (short profits when price falls)", pnl)
	}
}

func TestEvalCloseShortStopLoss(t *testing.T) {
	// short SL at 0.50*1.02 = 0.51; price at/above -> sl, pnl -2%.
	closed, _, pnl, reason := evalClose(shortEpisode(), 0.51)
	if !closed || reason != "sl" {
		t.Fatalf("expected sl close, got closed=%v reason=%q", closed, reason)
	}
	if pnl < -2.01 || pnl > -1.99 {
		t.Fatalf("pnl = %v, want ~-2", pnl)
	}
}

func TestEvalCloseStaysOpenInsideBand(t *testing.T) {
	// long, price between SL(0.49) and TP(0.52) -> no close.
	if closed, _, _, _ := evalClose(longEpisode(), 0.505); closed {
		t.Fatal("long should stay open when price is inside the SL/TP band")
	}
	// short, same band -> no close.
	if closed, _, _, _ := evalClose(shortEpisode(), 0.495); closed {
		t.Fatal("short should stay open when price is inside the SL/TP band")
	}
}

func TestEvalCloseNonEntryStaysOpen(t *testing.T) {
	// a HOLD episode shouldn't ever be recorded, but guard against closing one.
	ep := agent.TradeEpisode{ID: "x", EntryPrice: 0.5, Decision: agent.Decision{Action: agent.ActionHold}}
	if closed, _, _, _ := evalClose(ep, 0.40); closed {
		t.Fatal("non-entry episode should never close")
	}
}
