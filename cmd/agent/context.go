package main

import (
	"errors"
	"fmt"

	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/memory"
	"go-bot/pkg/exchange"
)

// contextLookback is how many prior candles define the breakout channel for regime
// classification (matches the live breakout strategy's 20-bar lookback).
const contextLookback = 20

// errFetch is returned (wrapped) when market data can't be fetched.
var errFetch = errors.New("market data fetch failed")

// buildContext snapshots the market situation for one symbol: it fetches recent candles
// (at the config interval), derives the [low, high] channel from the prior contextLookback
// bars (excluding the current one), classifies the regime, and recalls similar past
// episodes from memory. mem may be nil (recall skipped) for tests.
func buildContext(ex exchange.Exchange, symbol, interval string, mem *memory.Store, recallK int) (brain.Context, error) {
	candles, err := ex.GetKlines(symbol, interval, contextLookback+15)
	if err != nil {
		return brain.Context{}, fmt.Errorf("%w: %v", errFetch, err)
	}
	if len(candles) < contextLookback+1 {
		return brain.Context{}, fmt.Errorf("not enough candles for %s: got %d, need %d", symbol, len(candles), contextLookback+1)
	}

	price := candles[len(candles)-1].Close

	// Channel = high/low over the contextLookback bars BEFORE the current one.
	prior := candles[len(candles)-1-contextLookback : len(candles)-1]
	low, high := prior[0].Low, prior[0].High
	for _, c := range prior {
		if c.High > high {
			high = c.High
		}
		if c.Low < low {
			low = c.Low
		}
	}
	regime := classifyRegime(price, low, high)

	ctx := brain.Context{Symbol: symbol, Regime: regime, Price: price}
	if mem != nil {
		ctx.Past = mem.Recall(symbol, regime, recallK)
	}
	return ctx, nil
}
