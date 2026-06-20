package strategy

import (
	"fmt"

	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// Volatility-breakout tuning constants (not exposed in config, matching the
// other rule strategies which hardcode their parameters).
const (
	breakoutLookback   = 20  // rolling window for the high/low channel
	breakoutMinHistory = 35  // min candles before evaluating (keeps ATR + channel robust)
	breakoutRewardRisk = 2.0 // take-profit = stop-loss * this ratio (2.0R)
)

// VolatilityBreakout implements a two-sided rolling-channel breakout strategy:
// a close above the prior N-bar high goes LONG, a close below the prior N-bar low
// goes SHORT, each confirmed by above-average volume. Stop/target reuse the shared
// ATR sizing so position sizing stays consistent with the other strategies.
//
// The three tunable parameters live on the instance so a parameter sweep can vary
// them without touching the other strategies' shared constants. NewVolatilityBreakout
// uses the defaults, preserving the live/backtest behavior exactly.
type VolatilityBreakout struct {
	lookback   int     // rolling window for the high/low channel
	rewardRisk float64 // take-profit = stop-loss * this ratio
	atrK       float64 // stop-loss distance = atrK * ATR
}

// NewVolatilityBreakout creates a VolatilityBreakout with the default parameters.
func NewVolatilityBreakout() *VolatilityBreakout {
	return NewVolatilityBreakoutWithParams(breakoutLookback, breakoutRewardRisk, atrK)
}

// NewVolatilityBreakoutWithParams creates a VolatilityBreakout with explicit tuning
// parameters (used by the parameter sweep). Non-positive values fall back to the
// corresponding default so a bad combo can't produce a degenerate strategy.
func NewVolatilityBreakoutWithParams(lookback int, rewardRisk, atrMultiplier float64) *VolatilityBreakout {
	if lookback <= 0 {
		lookback = breakoutLookback
	}
	if rewardRisk <= 0 {
		rewardRisk = breakoutRewardRisk
	}
	if atrMultiplier <= 0 {
		atrMultiplier = atrK
	}
	return &VolatilityBreakout{lookback: lookback, rewardRisk: rewardRisk, atrK: atrMultiplier}
}

// Name returns the strategy identifier.
func (s *VolatilityBreakout) Name() string {
	return "volatility_breakout"
}

// rollingHigh returns the highest High over the `lookback` candles immediately
// BEFORE index idx (the current bar is excluded so the breakout level is fixed
// before the bar that breaks it). Assumes idx >= lookback.
func rollingHigh(candles []exchange.Candle, lookback, idx int) float64 {
	hi := candles[idx-lookback].High
	for i := idx - lookback + 1; i < idx; i++ {
		if candles[i].High > hi {
			hi = candles[i].High
		}
	}
	return hi
}

// rollingLow returns the lowest Low over the `lookback` candles immediately
// BEFORE index idx (current bar excluded). Assumes idx >= lookback.
func rollingLow(candles []exchange.Candle, lookback, idx int) float64 {
	lo := candles[idx-lookback].Low
	for i := idx - lookback + 1; i < idx; i++ {
		if candles[i].Low < lo {
			lo = candles[i].Low
		}
	}
	return lo
}

// avgVolume returns the mean Volume over the `lookback` candles immediately
// BEFORE index idx (current bar excluded). Assumes idx >= lookback.
func avgVolume(candles []exchange.Candle, lookback, idx int) float64 {
	var sum float64
	for i := idx - lookback; i < idx; i++ {
		sum += candles[i].Volume
	}
	return sum / float64(lookback)
}

// Evaluate analyzes candles to make a trading decision.
func (s *VolatilityBreakout) Evaluate(symbol string, candles []exchange.Candle) (*Decision, error) {
	n := len(candles)
	// Need the channel lookback plus ATR warmup headroom (matches the default
	// breakoutMinHistory of 35 at lookback 20).
	minHistory := s.lookback + 15
	if minHistory < breakoutMinHistory {
		minHistory = breakoutMinHistory
	}
	if n < minHistory {
		return &Decision{
			Decision:  HOLD,
			Leverage:  1,
			Reasoning: fmt.Sprintf("Insufficient historical data for %s: got %d, need at least %d candles", symbol, n, minHistory),
		}, nil
	}

	latest := n - 1
	currPrice := candles[latest].Close

	// ATR-based stop, written unconditionally (same convention as the other strategies).
	atr, err := indicators.CalculateATR(candles, atrPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate ATR: %v", err)
	}
	stopLoss := atrStopLossPctK(atr[latest], currPrice, s.atrK)
	takeProfit := stopLoss * s.rewardRisk

	upperChannel := rollingHigh(candles, s.lookback, latest)
	lowerChannel := rollingLow(candles, s.lookback, latest)
	avgVol := avgVolume(candles, s.lookback, latest)
	currVol := candles[latest].Volume

	// Volume confirmation: the breakout bar must trade on at least average volume.
	// When the feed carries no volume (avgVol == 0, e.g. some backtest data), the
	// filter is treated as satisfied rather than blocking every signal.
	volumeConfirmed := avgVol <= 0 || currVol >= avgVol

	decision := HOLD
	confidence := 0.5
	reason := fmt.Sprintf("Price (%.4f) inside breakout channel [%.4f, %.4f]. Waiting for a confirmed break.",
		currPrice, lowerChannel, upperChannel)

	if currPrice > upperChannel {
		if volumeConfirmed {
			decision = LONG
			confidence = 0.8
			reason = fmt.Sprintf("Close (%.4f) broke above %d-bar high (%.4f) on volume %.0f (avg %.0f). Bullish breakout.",
				currPrice, s.lookback, upperChannel, currVol, avgVol)
		} else {
			reason = fmt.Sprintf("Close (%.4f) broke above %d-bar high (%.4f) but volume %.0f < avg %.0f. Unconfirmed.",
				currPrice, s.lookback, upperChannel, currVol, avgVol)
		}
	} else if currPrice < lowerChannel {
		if volumeConfirmed {
			decision = SHORT
			confidence = 0.8
			reason = fmt.Sprintf("Close (%.4f) broke below %d-bar low (%.4f) on volume %.0f (avg %.0f). Bearish breakdown.",
				currPrice, s.lookback, lowerChannel, currVol, avgVol)
		} else {
			reason = fmt.Sprintf("Close (%.4f) broke below %d-bar low (%.4f) but volume %.0f < avg %.0f. Unconfirmed.",
				currPrice, s.lookback, lowerChannel, currVol, avgVol)
		}
	}

	return &Decision{
		Decision:      decision,
		Leverage:      3, // Standard safe leverage
		Confidence:    confidence,
		TakeProfitPct: takeProfit,
		StopLossPct:   stopLoss,
		Reasoning:     reason,
	}, nil
}
