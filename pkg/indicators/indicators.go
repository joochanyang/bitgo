package indicators

import (
	"fmt"
	"go-bot/pkg/exchange"
	"math"
)

// CalculateEMA computes the Exponential Moving Average for a given period
func CalculateEMA(candles []exchange.Candle, period int) ([]float64, error) {
	if len(candles) < period {
		return nil, fmt.Errorf("insufficient candles for EMA calculation: got %d, need at least %d", len(candles), period)
	}

	ema := make([]float64, len(candles))
	multiplier := 2.0 / (float64(period) + 1.0)

	// Step 1: Calculate Simple Moving Average for the first value
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += candles[i].Close
	}
	sma := sum / float64(period)
	ema[period-1] = sma

	// Step 2: Calculate EMA for subsequent values
	for i := period; i < len(candles); i++ {
		ema[i] = (candles[i].Close * multiplier) + (ema[i-1] * (1 - multiplier))
	}

	return ema, nil
}

// CalculateRSI computes the Relative Strength Index (RSI-14)
func CalculateRSI(candles []exchange.Candle, period int) ([]float64, error) {
	if len(candles) < period+1 {
		return nil, fmt.Errorf("insufficient candles for RSI calculation: got %d, need at least %d", len(candles), period+1)
	}

	rsi := make([]float64, len(candles))
	gains := make([]float64, len(candles))
	losses := make([]float64, len(candles))

	// Calculate change, gain, and loss
	for i := 1; i < len(candles); i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains[i] = change
			losses[i] = 0
		} else {
			gains[i] = 0
			losses[i] = -change
		}
	}

	// Calculate initial average gain/loss (SMA)
	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		rsi[period] = 100
	} else {
		rsi[period] = 100 - (100 / (1 + (avgGain / avgLoss)))
	}

	// Wilder's smoothing technique for subsequent values
	for i := period + 1; i < len(candles); i++ {
		avgGain = ((avgGain * float64(period-1)) + gains[i]) / float64(period)
		avgLoss = ((avgLoss * float64(period-1)) + losses[i]) / float64(period)

		if avgLoss == 0 {
			rsi[i] = 100
		} else {
			rsi[i] = 100 - (100 / (1 + (avgGain / avgLoss)))
		}
	}

	return rsi, nil
}

// CalculateATR computes the Average True Range (Wilder) for a given period.
//
// True Range (TR) at candle i is max(high-low, |high-prevClose|, |low-prevClose|).
// The first ATR value is seeded with the simple average of the first `period` TR
// values, then smoothed forward with Wilder's RMA:
//
//	ATR[i] = (ATR[i-1]*(period-1) + TR[i]) / period
//
// The returned slice has the same length as candles. Index i holds the ATR at
// candle i. Leading warmup indices (0..period-1) are left at their zero value,
// matching the alignment convention of CalculateRSI (first valid value at index
// `period`). Requires at least period+1 candles, since TR needs a previous close.
func CalculateATR(candles []exchange.Candle, period int) ([]float64, error) {
	if len(candles) < period+1 {
		return nil, fmt.Errorf("insufficient candles for ATR calculation: got %d, need at least %d", len(candles), period+1)
	}

	atr := make([]float64, len(candles))
	tr := make([]float64, len(candles))

	// Calculate True Range from index 1 (needs previous close)
	for i := 1; i < len(candles); i++ {
		highLow := candles[i].High - candles[i].Low
		highPrevClose := math.Abs(candles[i].High - candles[i-1].Close)
		lowPrevClose := math.Abs(candles[i].Low - candles[i-1].Close)
		tr[i] = math.Max(highLow, math.Max(highPrevClose, lowPrevClose))
	}

	// Seed the first ATR with the simple average of TR[1..period]
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += tr[i]
	}
	atr[period] = sum / float64(period)

	// Wilder's smoothing for subsequent values
	for i := period + 1; i < len(candles); i++ {
		atr[i] = ((atr[i-1] * float64(period-1)) + tr[i]) / float64(period)
	}

	return atr, nil
}

// MACDResult holds the computed MACD values
type MACDResult struct {
	MACD      []float64 // MACD Line (EMA12 - EMA26)
	Signal    []float64 // Signal Line (EMA9 of MACD)
	Histogram []float64 // Histogram (MACD - Signal)
}

// CalculateMACD computes the MACD line, Signal line, and Histogram
func CalculateMACD(candles []exchange.Candle, shortPeriod, longPeriod, signalPeriod int) (*MACDResult, error) {
	if len(candles) < longPeriod+signalPeriod {
		return nil, fmt.Errorf("insufficient candles for MACD calculation: got %d, need at least %d", len(candles), longPeriod+signalPeriod)
	}

	// Calculate short EMA (12)
	emaShort, err := CalculateEMA(candles, shortPeriod)
	if err != nil {
		return nil, err
	}

	// Calculate long EMA (26)
	emaLong, err := CalculateEMA(candles, longPeriod)
	if err != nil {
		return nil, err
	}

	// Calculate MACD line (EMA12 - EMA26)
	macdLine := make([]float64, len(candles))
	for i := longPeriod - 1; i < len(candles); i++ {
		macdLine[i] = emaShort[i] - emaLong[i]
	}

	// Calculate Signal line (EMA9 of MACD line)
	// We need to slice the macdLine to calculate its EMA
	// The macdLine is only valid from index (longPeriod - 1)
	macdValidSlice := macdLine[longPeriod-1:]
	macdCandleSlice := make([]exchange.Candle, len(macdValidSlice))
	for i, val := range macdValidSlice {
		macdCandleSlice[i] = exchange.Candle{Close: val}
	}

	emaSignalValid, err := CalculateEMA(macdCandleSlice, signalPeriod)
	if err != nil {
		return nil, err
	}

	// Build full signal and histogram slices matching original candles length
	signalLine := make([]float64, len(candles))
	histogram := make([]float64, len(candles))

	// The signal line starts being valid at longPeriod - 1 + signalPeriod - 1
	startIdx := longPeriod - 1 + signalPeriod - 1
	for i := startIdx; i < len(candles); i++ {
		// Index in emaSignalValid corresponding to original index i
		validIndex := i - (longPeriod - 1)
		signalLine[i] = emaSignalValid[validIndex]
		histogram[i] = macdLine[i] - signalLine[i]
	}

	return &MACDResult{
		MACD:      macdLine,
		Signal:    signalLine,
		Histogram: histogram,
	}, nil
}

// BollingerBandsResult holds the computed Bollinger Bands
type BollingerBandsResult struct {
	Upper  []float64
	Middle []float64
	Lower  []float64
}

// CalculateBollingerBands calculates the Upper, Middle (SMA), and Lower Bollinger Bands
func CalculateBollingerBands(candles []exchange.Candle, period int, stdDevMultiplier float64) (*BollingerBandsResult, error) {
	if len(candles) < period {
		return nil, fmt.Errorf("insufficient candles for Bollinger Bands: got %d, need at least %d", len(candles), period)
	}

	upper := make([]float64, len(candles))
	middle := make([]float64, len(candles))
	lower := make([]float64, len(candles))

	for i := period - 1; i < len(candles); i++ {
		// Calculate SMA (Middle Band)
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			sum += candles[j].Close
		}
		sma := sum / float64(period)
		middle[i] = sma

		// Calculate Standard Deviation
		varianceSum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := candles[j].Close - sma
			varianceSum += diff * diff
		}
		variance := varianceSum / float64(period)
		stdDev := math.Sqrt(variance)

		upper[i] = sma + (stdDevMultiplier * stdDev)
		lower[i] = sma - (stdDevMultiplier * stdDev)
	}

	return &BollingerBandsResult{
		Upper:  upper,
		Middle: middle,
		Lower:  lower,
	}, nil
}
