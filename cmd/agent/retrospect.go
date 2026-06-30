package main

import (
	"log"
	"time"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/memory"
)

// evalClose simulates whether an open paper episode would have closed at the given price,
// based on the entry, direction, and the SL/TP percentages the council chose. Paper mode
// has no real position, so the agent reconstructs the SL/TP levels from stored data and
// checks them tick-by-tick (it does not inspect intra-tick candle highs/lows — a coarse
// approximation that's adequate for learning, tightened when a real executor lands).
//
// Returns closed=false (and zero outcome) when the episode is not an entry or price is
// still inside the SL/TP band. pnlPct is signed by direction: a long profits as price
// rises, a short as it falls.
func evalClose(ep agent.TradeEpisode, price float64) (closed bool, exitPrice, pnlPct float64, reason string) {
	entry := ep.EntryPrice
	if entry <= 0 {
		return false, 0, 0, ""
	}
	switch ep.Decision.Action {
	case agent.ActionEnterLong:
		sl := entry * (1 - ep.Decision.StopLossPct/100)
		tp := entry * (1 + ep.Decision.TakeProfitPct/100)
		if price <= sl {
			return true, price, (price - entry) / entry * 100, "sl"
		}
		if price >= tp {
			return true, price, (price - entry) / entry * 100, "tp"
		}
	case agent.ActionEnterShort:
		sl := entry * (1 + ep.Decision.StopLossPct/100)
		tp := entry * (1 - ep.Decision.TakeProfitPct/100)
		if price >= sl {
			return true, price, (entry - price) / entry * 100, "sl"
		}
		if price <= tp {
			return true, price, (entry - price) / entry * 100, "tp"
		}
	}
	return false, 0, 0, ""
}

// retrospect closes any open episodes for one symbol that the current price would have
// stopped out or taken profit. It runs at the start of each tick (before the council
// deliberates) so a fresh outcome is already recorded when memory.Recall feeds the
// council its lessons. now is injected for deterministic timestamps in tests.
func retrospect(mem *memory.Store, symbol string, price float64, now time.Time) {
	eps, err := mem.All()
	if err != nil {
		log.Printf("[%s] retrospect: load memory: %v", symbol, err)
		return
	}
	for _, ep := range eps {
		if ep.Closed || ep.Symbol != symbol {
			continue
		}
		closed, exit, pnl, reason := evalClose(ep, price)
		if !closed {
			continue
		}
		if err := mem.Close(ep.ID, now, exit, pnl, reason); err != nil {
			log.Printf("[%s] retrospect: close %s: %v", symbol, ep.ID, err)
			continue
		}
		log.Printf("[%s] [PAPER CLOSE] %s %s @%.6f pnl=%+.2f%% (episode %s)",
			symbol, ep.Decision.Action, reason, exit, pnl, ep.ID)
	}
}
