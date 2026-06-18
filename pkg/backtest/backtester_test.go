package backtest

import (
	"testing"
	"time"

	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// openOnceStrategy opens a single LONG on its first non-warmup evaluation, then HOLDs.
// SL/TP are set absurdly wide so the LIQUIDATION path (not SL) is what fires.
type openOnceStrategy struct {
	opened bool
	lev    int
}

func (s *openOnceStrategy) Name() string { return "open-once" }
func (s *openOnceStrategy) Evaluate(symbol string, candles []exchange.Candle) (*strategy.Decision, error) {
	if s.opened {
		return &strategy.Decision{Decision: strategy.HOLD}, nil
	}
	s.opened = true
	return &strategy.Decision{
		Decision:      strategy.LONG,
		Leverage:      s.lev,
		StopLossPct:   90, // absurdly wide so liquidation fires before SL
		TakeProfitPct: 90,
	}, nil
}

// makeCandles builds n flat candles at price `flat`, then one crash candle whose Low is `crashLow`.
func makeCandles(n int, flat, crashLow float64) []exchange.Candle {
	out := make([]exchange.Candle, 0, n+1)
	base := time.Unix(1_700_000_000, 0)
	for i := 0; i < n; i++ {
		out = append(out, exchange.Candle{
			Time: base.Add(time.Duration(i) * time.Hour),
			Open: flat, High: flat, Low: flat, Close: flat,
		})
	}
	// Crash candle: opens flat, low dives to crashLow, closes at crashLow.
	out = append(out, exchange.Candle{
		Time: base.Add(time.Duration(n) * time.Hour),
		Open: flat, High: flat, Low: crashLow, Close: crashLow,
	})
	return out
}

// TestLiquidationFloorsBalance: with 5x leverage, a >20% drop liquidates the position;
// balance must not go negative and a LIQUIDATION trade must be recorded.
func TestLiquidationFloorsBalance(t *testing.T) {
	// 5x leverage -> liquidation at -20% (price 80). Crash to 50 (-50%) blows past it.
	candles := makeCandles(60, 100.0, 50.0)
	strat := &openOnceStrategy{lev: 5}

	report, err := RunBacktest("WLDUSDT", candles, strat, 10000.0, 5.0, 5, 0.0006)
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}

	if report.FinalBalance < 0 {
		t.Errorf("balance went negative: %.2f", report.FinalBalance)
	}

	foundLiquidation := false
	for _, tr := range report.Trades {
		if tr.ExitReason == "LIQUIDATION" {
			foundLiquidation = true
			if tr.PnL >= 0 {
				t.Errorf("liquidation PnL should be a loss, got %.2f", tr.PnL)
			}
		}
	}
	if !foundLiquidation {
		t.Errorf("expected a LIQUIDATION trade, got trades: %+v", report.Trades)
	}

	// Drawdown must be a sane percentage (0..100), never inverted/exploded.
	if report.MaxDrawdownPct < 0 || report.MaxDrawdownPct > 100 {
		t.Errorf("drawdown out of range: %.2f%%", report.MaxDrawdownPct)
	}
}

// TestNoLiquidationOnMildMove: a small adverse move (within margin) must NOT liquidate.
func TestNoLiquidationOnMildMove(t *testing.T) {
	// 2x leverage -> liquidation at -50% (price 50). Dip to 95 (-5%) is harmless.
	candles := makeCandles(60, 100.0, 95.0)
	strat := &openOnceStrategy{lev: 2}

	report, err := RunBacktest("WLDUSDT", candles, strat, 10000.0, 5.0, 3, 0.0006)
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}
	for _, tr := range report.Trades {
		if tr.ExitReason == "LIQUIDATION" {
			t.Errorf("unexpected liquidation on a mild -5%% move: %+v", tr)
		}
	}
}

// alwaysHoldStrategy never trades — used to make split coverage deterministic (0 trades).
type alwaysHoldStrategy struct{}

func (alwaysHoldStrategy) Name() string { return "always-hold" }
func (alwaysHoldStrategy) Evaluate(symbol string, candles []exchange.Candle) (*strategy.Decision, error) {
	return &strategy.Decision{Decision: strategy.HOLD}, nil
}

// TestRunBacktestSplitCoverage: a 0.7 split produces two non-nil sub-reports, each
// carrying the resolved initial balance, with the split ratio echoed back.
func TestRunBacktestSplitCoverage(t *testing.T) {
	candles := makeCandles(199, 100.0, 100.0) // 200 flat candles, no crash
	rep, err := RunBacktestSplit("WLDUSDT", candles, alwaysHoldStrategy{}, 12345.0, 5.0, 3, 0.0006, 0.7)
	if err != nil {
		t.Fatalf("RunBacktestSplit: %v", err)
	}
	if rep.InSample == nil || rep.OutOfSample == nil {
		t.Fatalf("expected both sub-reports non-nil, got in=%v oos=%v", rep.InSample, rep.OutOfSample)
	}
	if rep.SplitRatio != 0.7 {
		t.Errorf("SplitRatio = %v, want 0.7", rep.SplitRatio)
	}
	if rep.InSample.InitialBalance != 12345.0 || rep.OutOfSample.InitialBalance != 12345.0 {
		t.Errorf("resolved balance must thread into both reports: in=%v oos=%v",
			rep.InSample.InitialBalance, rep.OutOfSample.InitialBalance)
	}
	// Hold strategy never trades.
	if rep.InSample.TotalTrades != 0 || rep.OutOfSample.TotalTrades != 0 {
		t.Errorf("hold strategy should produce 0 trades: in=%d oos=%d",
			rep.InSample.TotalTrades, rep.OutOfSample.TotalTrades)
	}
}

// TestRunBacktestSplitRejectsBadRatio: ratios that don't leave both segments enough
// warmup (or are out of (0,1)) must error.
func TestRunBacktestSplitRejectsBadRatio(t *testing.T) {
	candles := makeCandles(199, 100.0, 100.0)
	for _, ratio := range []float64{0, 1, -0.5, 1.5} {
		if _, err := RunBacktestSplit("WLDUSDT", candles, alwaysHoldStrategy{}, 10000.0, 5.0, 3, 0.0006, ratio); err == nil {
			t.Errorf("ratio %v: expected error, got nil", ratio)
		}
	}
}

// openOnceSL opens a single LONG with an explicit StopLossPct on its first
// evaluation, then HOLDs. Used to verify risk-based sizing.
type openOnceSL struct {
	opened bool
	lev    int
	slPct  float64
}

func (s *openOnceSL) Name() string { return "open-once-sl" }
func (s *openOnceSL) Evaluate(symbol string, candles []exchange.Candle) (*strategy.Decision, error) {
	if s.opened {
		return &strategy.Decision{Decision: strategy.HOLD}, nil
	}
	s.opened = true
	return &strategy.Decision{
		Decision:      strategy.LONG,
		Leverage:      s.lev,
		StopLossPct:   s.slPct,
		TakeProfitPct: 50, // far away so TP never fires in this test
	}, nil
}

// TestRiskBasedSizingLossEqualsRiskPct: with risk-based sizing, a stop-out must lose
// ~riskPct% of balance regardless of leverage. Open LONG at 100, SL 2% (=98), lev 3
// (liquidation at ~66.7, far below the stop), feeRate 0; a candle dips to 98 to fire SL.
func TestRiskBasedSizingLossEqualsRiskPct(t *testing.T) {
	// 60 flat warmup candles at 100, then a candle whose Low touches 98 (hits SL) and recovers.
	candles := makeCandles(60, 100.0, 98.0)
	// makeCandles sets the crash candle's Close to crashLow (98); the SL trigger uses Low<=98.
	strat := &openOnceSL{lev: 3, slPct: 2.0}

	const initialBalance = 10000.0
	const riskPct = 5.0
	report, err := RunBacktest("WLDUSDT", candles, strat, initialBalance, riskPct, 3, 0.0)
	if err != nil {
		t.Fatalf("RunBacktest: %v", err)
	}

	// Find the SL exit trade.
	var slTrade *BacktestTrade
	for i := range report.Trades {
		if report.Trades[i].ExitReason == "SL" {
			slTrade = &report.Trades[i]
			break
		}
	}
	if slTrade == nil {
		t.Fatalf("expected an SL exit trade, got trades: %+v", report.Trades)
	}

	// Loss at SL should be ~riskPct% of the initial balance (5% of 10000 = 500).
	wantLoss := initialBalance * (riskPct / 100.0)
	if diff := slTrade.PnL + wantLoss; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("SL loss = %.6f, want ~%.6f (riskPct%% of balance)", slTrade.PnL, -wantLoss)
	}
}
