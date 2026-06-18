package indicators

import (
	"math"
	"testing"
	"time"

	"go-bot/pkg/exchange"
)

// Helper to generate a sequence of mock candles with linear prices
func generateMockCandles(prices []float64) []exchange.Candle {
	candles := make([]exchange.Candle, len(prices))
	baseTime := time.Now().Add(-time.Duration(len(prices)) * time.Hour)
	for i, p := range prices {
		candles[i] = exchange.Candle{
			Time:  baseTime.Add(time.Duration(i) * time.Hour),
			Open:  p,
			High:  p + 1,
			Low:   p - 1,
			Close: p,
		}
	}
	return candles
}

func TestCalculateEMA(t *testing.T) {
	// Generate prices: 10, 11, 12, 13, 14, 15
	prices := []float64{10, 11, 12, 13, 14, 15}
	candles := generateMockCandles(prices)
	period := 3

	ema, err := CalculateEMA(candles, period)
	if err != nil {
		t.Fatalf("CalculateEMA failed: %v", err)
	}

	// First valid EMA should start at index period-1 (2)
	// Initial value at index 2 (period-1) is SMA of index 0, 1, 2 = (10+11+12)/3 = 11
	if math.Abs(ema[2]-11.0) > 0.0001 {
		t.Errorf("Expected SMA start to be 11.0, got %f", ema[2])
	}

	// Multiplier for period 3 is 2 / (3+1) = 0.5
	// EMA[3] = (13 * 0.5) + (11.0 * 0.5) = 6.5 + 5.5 = 12.0
	if math.Abs(ema[3]-12.0) > 0.0001 {
		t.Errorf("Expected EMA[3] to be 12.0, got %f", ema[3])
	}

	// EMA[4] = (14 * 0.5) + (12.0 * 0.5) = 7.0 + 6.0 = 13.0
	if math.Abs(ema[4]-13.0) > 0.0001 {
		t.Errorf("Expected EMA[4] to be 13.0, got %f", ema[4])
	}
}

func TestCalculateATR(t *testing.T) {
	// Hand-computable OHLC series with period 3.
	// prevClose is the Close of the previous candle.
	//   TR[i] = max(High-Low, |High-prevClose|, |Low-prevClose|)
	//   i=1: max(11-9,  |11-9|,  |9-9|)   = 2
	//   i=2: max(12-10, |12-10|, |10-10|) = 2
	//   i=3: max(13-11, |13-11|, |11-11|) = 2
	//   i=4: max(16-12, |16-12|, |12-12|) = 4
	candles := []exchange.Candle{
		{High: 10, Low: 8, Close: 9},
		{High: 11, Low: 9, Close: 10},
		{High: 12, Low: 10, Close: 11},
		{High: 13, Low: 11, Close: 12},
		{High: 16, Low: 12, Close: 13},
	}
	period := 3

	atr, err := CalculateATR(candles, period)
	if err != nil {
		t.Fatalf("CalculateATR failed: %v", err)
	}

	// Returned slice aligns with candles length
	if len(atr) != len(candles) {
		t.Fatalf("Expected ATR length %d, got %d", len(candles), len(atr))
	}

	// Warmup indices (0..period-1) are left at zero
	for i := 0; i < period; i++ {
		if atr[i] != 0 {
			t.Errorf("Expected warmup ATR[%d] to be 0, got %f", i, atr[i])
		}
	}

	// Seed: ATR[3] = (TR[1]+TR[2]+TR[3])/3 = (2+2+2)/3 = 2.0
	if math.Abs(atr[3]-2.0) > 0.0001 {
		t.Errorf("Expected ATR[3] (Wilder seed) to be 2.0, got %f", atr[3])
	}

	// Wilder smoothing: ATR[4] = (ATR[3]*(3-1) + TR[4])/3 = (2.0*2 + 4)/3 = 8/3
	if math.Abs(atr[4]-(8.0/3.0)) > 0.0001 {
		t.Errorf("Expected ATR[4] to be %f, got %f", 8.0/3.0, atr[4])
	}
}

func TestCalculateATRInsufficientCandles(t *testing.T) {
	// period+1 candles are required; period candles must error.
	candles := generateMockCandles([]float64{10, 11, 12})
	if _, err := CalculateATR(candles, 3); err == nil {
		t.Errorf("Expected error for insufficient candles (got 3, need 4), got nil")
	}
}

func TestCalculateRSI(t *testing.T) {
	// Sequence of prices to generate gain and losses
	prices := []float64{
		44.33, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10,
		45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28,
		46.28, 46.00,
	}
	candles := generateMockCandles(prices)
	period := 14

	rsi, err := CalculateRSI(candles, period)
	if err != nil {
		t.Fatalf("CalculateRSI failed: %v", err)
	}

	// First valid RSI should be at index period (14)
	val := rsi[period]
	if val < 0 || val > 100 {
		t.Errorf("Expected RSI to be between 0 and 100, got %f", val)
	}
}
