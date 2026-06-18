package strategy

import (
	"fmt"
	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// TrendFollowing implements a mechanical trend-following trading strategy
type TrendFollowing struct{}

// NewTrendFollowing creates a new instance of the TrendFollowing strategy
func NewTrendFollowing() *TrendFollowing {
	return &TrendFollowing{}
}

// Name returns the strategy identifier
func (s *TrendFollowing) Name() string {
	return "trend_following"
}

// Evaluate analyzes candles to make a trading decision
func (s *TrendFollowing) Evaluate(symbol string, candles []exchange.Candle) (*Decision, error) {
	n := len(candles)
	if n < 35 { // Minimum required history for indicators
		return &Decision{
			Decision:  HOLD,
			Leverage:  1,
			Reasoning: fmt.Sprintf("Insufficient historical data for %s: got %d, need at least 35 candles", symbol, n),
		}, nil
	}

	// Calculate technical indicators
	rsi, err := indicators.CalculateRSI(candles, 14)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate RSI: %v", err)
	}

	ema9, err := indicators.CalculateEMA(candles, 9)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate EMA(9): %v", err)
	}

	ema21, err := indicators.CalculateEMA(candles, 21)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate EMA(21): %v", err)
	}

	macdRes, err := indicators.CalculateMACD(candles, 12, 26, 9)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate MACD: %v", err)
	}

	latest := n - 1
	prev := n - 2

	// Check for EMA Crossover
	// Bullish Cross: EMA9 was below/equal EMA21, now EMA9 is above EMA21
	bullishCross := ema9[prev] <= ema21[prev] && ema9[latest] > ema21[latest]
	// Bearish Cross: EMA9 was above/equal EMA21, now EMA9 is below EMA21
	bearishCross := ema9[prev] >= ema21[prev] && ema9[latest] < ema21[latest]

	currRSI := rsi[latest]
	currMACDHist := macdRes.Histogram[latest]
	prevMACDHist := macdRes.Histogram[prev]

	decision := HOLD
	reason := "Market trend is neutral. Waiting for EMA9/EMA21 crossover."
	confidence := 0.5
	// ATR-based stop: distance = atrK * ATR, expressed as a percent of price.
	// TakeProfit preserves this strategy's original 3.5/1.5 (2.33R) reward:risk ratio.
	atr, err := indicators.CalculateATR(candles, atrPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate ATR: %v", err)
	}
	stopLoss := atrStopLossPct(atr[latest], candles[latest].Close)
	takeProfit := stopLoss * (3.5 / 1.5)

	if bullishCross {
		// Filter with MACD (increasing momentum) and RSI (not overbought)
		if currMACDHist > 0 || currMACDHist > prevMACDHist {
			if currRSI < 65 {
				decision = LONG
				confidence = 0.8
				reason = fmt.Sprintf("EMA9 crossed above EMA21 (Bullish). MACD Hist is supportive (%.4f), RSI (%.1f) is in buy zone.", currMACDHist, currRSI)
			} else {
				reason = fmt.Sprintf("EMA9 crossed above EMA21, but filtered: RSI (%.1f) indicates overbought conditions.", currRSI)
			}
		} else {
			reason = "EMA9 crossed above EMA21, but filtered: MACD histogram momentum is declining."
		}
	} else if bearishCross {
		// Filter with MACD (decreasing momentum) and RSI (not oversold)
		if currMACDHist < 0 || currMACDHist < prevMACDHist {
			if currRSI > 35 {
				decision = SHORT
				confidence = 0.8
				reason = fmt.Sprintf("EMA9 crossed below EMA21 (Bearish). MACD Hist is supportive (%.4f), RSI (%.1f) is in sell zone.", currMACDHist, currRSI)
			} else {
				reason = fmt.Sprintf("EMA9 crossed below EMA21, but filtered: RSI (%.1f) indicates oversold conditions.", currRSI)
			}
		} else {
			reason = "EMA9 crossed below EMA21, but filtered: MACD histogram momentum is rising."
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
