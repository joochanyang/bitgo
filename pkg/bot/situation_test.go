package bot

import (
	"strings"
	"testing"
)

// TestDescribeSituationWaiting: no position, last decision HOLD → a beginner-friendly
// "waiting for a signal" message that names the symbol and the current price.
func TestDescribeSituationWaiting(t *testing.T) {
	mv := MarketView{
		Symbol:       "WLDUSDT",
		IsRunning:    true, // a waiting bot is running but holds nothing yet
		HasPosition:  false,
		Decision:     "HOLD",
		CurrentPrice: 0.61,
	}
	got := describeSituation(mv)
	if got.Headline == "" {
		t.Fatal("Headline empty")
	}
	// Must read as "waiting", mention the symbol, and not claim a position.
	if !strings.Contains(got.Headline, "대기") && !strings.Contains(got.Detail, "대기") {
		t.Errorf("waiting state should mention 대기; got headline=%q detail=%q", got.Headline, got.Detail)
	}
	if !strings.Contains(got.Detail, "WLDUSDT") {
		t.Errorf("detail should name the symbol; got %q", got.Detail)
	}
}

// TestDescribeSituationHoldingLong: an open long position → message states we are
// holding a long, shows entry price and (since SL/TP known) the exit levels.
func TestDescribeSituationHoldingLong(t *testing.T) {
	mv := MarketView{
		Symbol:        "WLDUSDT",
		HasPosition:   true,
		Side:          "LONG",
		EntryPrice:    0.61,
		CurrentPrice:  0.63,
		StopLossPrice: 0.59,
		TakeProfitPct: 0.65,
		UnrealizedPnL: 12.3,
	}
	got := describeSituation(mv)
	if !strings.Contains(got.Headline, "보유") && !strings.Contains(got.Headline, "롱") {
		t.Errorf("holding-long headline should mention 보유/롱; got %q", got.Headline)
	}
	// Detail should reference entry price so a beginner sees where they got in.
	if !strings.Contains(got.Detail, "0.61") {
		t.Errorf("detail should show entry price 0.61; got %q", got.Detail)
	}
	// Profit state: current 0.63 > entry 0.61, PnL positive → mention 수익.
	if !strings.Contains(got.Detail, "수익") {
		t.Errorf("detail should note it is in profit; got %q", got.Detail)
	}
}

// TestDescribeSituationHoldingShortLoss: short position currently at a loss.
func TestDescribeSituationHoldingShortLoss(t *testing.T) {
	mv := MarketView{
		Symbol:        "WLDUSDT",
		HasPosition:   true,
		Side:          "SHORT",
		EntryPrice:    0.60,
		CurrentPrice:  0.62, // short losing as price rises
		UnrealizedPnL: -8.0,
	}
	got := describeSituation(mv)
	if !strings.Contains(got.Headline, "숏") && !strings.Contains(got.Headline, "보유") {
		t.Errorf("short headline should mention 숏/보유; got %q", got.Headline)
	}
	if !strings.Contains(got.Detail, "손실") {
		t.Errorf("losing position detail should note 손실; got %q", got.Detail)
	}
}

// TestDescribeSituationStopped: engine not running → message says it is stopped.
func TestDescribeSituationStopped(t *testing.T) {
	mv := MarketView{Symbol: "WLDUSDT", IsRunning: false}
	got := describeSituation(mv)
	if !strings.Contains(got.Headline, "정지") && !strings.Contains(got.Headline, "꺼") {
		t.Errorf("stopped headline should mention 정지/꺼짐; got %q", got.Headline)
	}
}
