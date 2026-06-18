package strategy

import (
	"fmt"
	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// MeanReversion implements a Bollinger Bands + RSI mean reversion trading strategy
type MeanReversion struct{}

// NewMeanReversion creates a new instance of the MeanReversion strategy
func NewMeanReversion() *MeanReversion {
	return &MeanReversion{}
}

// Name returns the strategy identifier
func (s *MeanReversion) Name() string {
	return "mean_reversion"
}

// Evaluate analyzes candles to make a trading decision
func (s *MeanReversion) Evaluate(symbol string, candles []exchange.Candle) (*Decision, error) {
	n := len(candles)
	if n < 20 { // Minimum required history for Bollinger Bands
		return &Decision{
			Decision:  HOLD,
			Leverage:  1,
			Reasoning: fmt.Sprintf("Insufficient historical data for %s: got %d, need at least 20 candles", symbol, n),
		}, nil
	}

	// Calculate indicators
	rsi, err := indicators.CalculateRSI(candles, 14)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate RSI: %v", err)
	}

	bb, err := indicators.CalculateBollingerBands(candles, 20, 2.0)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate Bollinger Bands: %v", err)
	}

	latest := n - 1
	currPrice := candles[latest].Close
	currRSI := rsi[latest]
	upperBB := bb.Upper[latest]
	lowerBB := bb.Lower[latest]
	middleBB := bb.Middle[latest]

	decision := HOLD
	reason := fmt.Sprintf("Price is within Bollinger Bands range (Lower: %.4f, Upper: %.4f). Current Close: %.4f.", lowerBB, upperBB, currPrice)
	confidence := 0.5
	// ATR-based stop: distance = atrK * ATR, expressed as a percent of price.
	// TakeProfit preserves this strategy's original 2.5/1.25 (2.0R) reward:risk ratio.
	atr, err := indicators.CalculateATR(candles, atrPeriod)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate ATR: %v", err)
	}
	stopLoss := atrStopLossPct(atr[latest], currPrice)
	takeProfit := stopLoss * (2.5 / 1.25)

	if currPrice <= lowerBB {
		// Oversold confirmation
		if currRSI <= 35 {
			decision = LONG
			confidence = 0.8
			reason = fmt.Sprintf("Price (%.4f) touched or crossed Lower BB (%.4f) with oversold RSI (%.1f). Expecting reversion to Middle BB (%.4f).",
				currPrice, lowerBB, currRSI, middleBB)
		} else {
			reason = fmt.Sprintf("Price (%.4f) touched Lower BB, but filtered: RSI (%.1f) is not oversold.", currPrice, currRSI)
		}
	} else if currPrice >= upperBB {
		// Overbought confirmation
		if currRSI >= 65 {
			decision = SHORT
			confidence = 0.8
			reason = fmt.Sprintf("Price (%.4f) touched or crossed Upper BB (%.4f) with overbought RSI (%.1f). Expecting reversion to Middle BB (%.4f).",
				currPrice, upperBB, currRSI, middleBB)
		} else {
			reason = fmt.Sprintf("Price (%.4f) touched Upper BB, but filtered: RSI (%.1f) is not overbought.", currPrice, currRSI)
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
