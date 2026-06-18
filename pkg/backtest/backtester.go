package backtest

import (
	"fmt"
	"math"
	"time"

	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// BacktestTrade represents a single completed trade in simulation
type BacktestTrade struct {
	Symbol     string    `json:"symbol"`
	Side       string    `json:"side"` // "LONG", "SHORT"
	EntryPrice float64   `json:"entry_price"`
	ExitPrice  float64   `json:"exit_price"`
	Size       float64   `json:"size"`
	PnL        float64   `json:"pnl"`
	EntryTime  time.Time `json:"entry_time"`
	ExitTime   time.Time `json:"exit_time"`
	ExitReason string    `json:"exit_reason"` // "SL", "TP", "CLOSE", "SWITCH", "FORCE_CLOSE"
}

// BacktestReport holds the performance summary of a backtest run
type BacktestReport struct {
	StrategyName   string          `json:"strategy_name"`
	Symbol         string          `json:"symbol"`
	InitialBalance float64         `json:"initial_balance"`
	FinalBalance   float64         `json:"final_balance"`
	TotalReturnPct float64         `json:"total_return_pct"`
	MaxDrawdownPct float64         `json:"max_drawdown_pct"`
	WinRatePct     float64         `json:"win_rate_pct"`
	ProfitFactor   float64         `json:"profit_factor"`
	SharpeRatio    float64         `json:"sharpe_ratio"`
	TotalTrades    int             `json:"total_trades"`
	Trades         []BacktestTrade `json:"trades"`
}

type simPosition struct {
	Side             string
	Size             float64
	EntryPrice       float64
	StopLossPrice    float64
	TakeProfitPrice  float64
	LiquidationPrice float64
	Margin           float64 // deposited margin = notional / leverage
	EntryTime        time.Time
}

// backtestWarmup is the number of leading candles RunBacktest consumes before it
// begins trading (indicator warmup). Kept in sync with startIdx below.
const backtestWarmup = 50

// SplitReport holds in-sample / out-of-sample backtest results for one run, so a
// user can spot overfitting (in-sample looks great, out-of-sample doesn't).
type SplitReport struct {
	SplitRatio  float64         `json:"split_ratio"`
	InSample    *BacktestReport `json:"in_sample"`
	OutOfSample *BacktestReport `json:"out_of_sample"`
}

// RunBacktestSplit runs the strategy on an in-sample segment (the first splitRatio
// of the candles) and an out-of-sample segment (the remainder), returning a report
// for each. The out-of-sample segment includes the trailing backtestWarmup candles
// from the in-sample tail so its indicators warm up at the boundary.
//
// NOTE: the same strategy instance is reused for both segments; strategies must be
// stateless across Evaluate calls (the rule strategies recompute from `candles` each
// call, so this holds).
func RunBacktestSplit(symbol string, candles []exchange.Candle, strat strategy.Strategy, initialBalance float64, riskPct float64, leverage int, feeRate float64, splitRatio float64) (*SplitReport, error) {
	if splitRatio <= 0 || splitRatio >= 1 {
		return nil, fmt.Errorf("split_ratio must be between 0 and 1 (exclusive), got %g", splitRatio)
	}
	n := len(candles)
	boundary := int(float64(n) * splitRatio)

	// In-sample is candles[:boundary] (needs warmup+1); out-of-sample reuses the
	// trailing warmup candles from the boundary, so boundary <= n-1 floors the OOS
	// segment at warmup+1 too.
	if boundary < backtestWarmup+1 || boundary > n-1 {
		return nil, fmt.Errorf("split_ratio %g leaves a segment too small for the %d-candle warmup (n=%d, boundary=%d)", splitRatio, backtestWarmup, n, boundary)
	}

	inSample := candles[:boundary]
	outOfSample := candles[boundary-backtestWarmup:]

	inReport, err := RunBacktest(symbol, inSample, strat, initialBalance, riskPct, leverage, feeRate)
	if err != nil {
		return nil, fmt.Errorf("in-sample: %w", err)
	}
	oosReport, err := RunBacktest(symbol, outOfSample, strat, initialBalance, riskPct, leverage, feeRate)
	if err != nil {
		return nil, fmt.Errorf("out-of-sample: %w", err)
	}

	return &SplitReport{
		SplitRatio:  splitRatio,
		InSample:    inReport,
		OutOfSample: oosReport,
	}, nil
}

// RunBacktest runs a historical strategy simulation
func RunBacktest(symbol string, candles []exchange.Candle, strat strategy.Strategy, initialBalance float64, riskPct float64, leverage int, feeRate float64) (*BacktestReport, error) {
	n := len(candles)
	// We need enough history to calculate indicators (min 50)
	startIdx := backtestWarmup
	if n < startIdx {
		return nil, fmt.Errorf("insufficient candles for backtest: got %d, need at least %d", n, startIdx)
	}

	balance := initialBalance
	peakBalance := initialBalance
	maxDrawdown := 0.0

	var activePos *simPosition
	var completedTrades []BacktestTrade

	for i := startIdx; i < n; i++ {
		currCandle := candles[i]
		currClose := currCandle.Close

		// Track peak balance (including unrealized PnL of active position)
		currentEquity := balance
		if activePos != nil {
			var pnl float64
			if activePos.Side == "LONG" {
				pnl = (currClose - activePos.EntryPrice) * activePos.Size
			} else {
				pnl = (activePos.EntryPrice - currClose) * activePos.Size
			}
			currentEquity += pnl
		}
		// Equity can't fall below zero (a position is liquidated before that); floor it so a
		// transient negative mark-to-market never inverts/explodes the drawdown percentage.
		if currentEquity < 0 {
			currentEquity = 0
		}
		if currentEquity > peakBalance {
			peakBalance = currentEquity
		}
		if peakBalance > 0 {
			drawdown := (peakBalance - currentEquity) / peakBalance * 100.0
			if drawdown > maxDrawdown {
				maxDrawdown = drawdown
			}
		}

		// 0. Check liquidation FIRST: if the adverse extreme reaches the liquidation price
		//    before any stop, the position is force-closed and the deposited margin is lost.
		//    This prevents balance from going arbitrarily negative on leveraged losses.
		if activePos != nil && activePos.LiquidationPrice > 0 {
			liquidated := (activePos.Side == "LONG" && currCandle.Low <= activePos.LiquidationPrice) ||
				(activePos.Side == "SHORT" && currCandle.High >= activePos.LiquidationPrice)
			// Only treat as liquidation if a stop would NOT have fired first (i.e. the stop is
			// beyond the liquidation price or absent). Otherwise the normal SL/TP block handles it.
			stopFirst := false
			if activePos.Side == "LONG" {
				stopFirst = activePos.StopLossPrice > 0 && activePos.StopLossPrice >= activePos.LiquidationPrice && currCandle.Low <= activePos.StopLossPrice
			} else {
				stopFirst = activePos.StopLossPrice > 0 && activePos.StopLossPrice <= activePos.LiquidationPrice && currCandle.High >= activePos.StopLossPrice
			}

			if liquidated && !stopFirst {
				// Loss equals the full deposited margin; balance floored at 0.
				balance -= activePos.Margin
				if balance < 0 {
					balance = 0
				}
				completedTrades = append(completedTrades, BacktestTrade{
					Symbol:     symbol,
					Side:       activePos.Side,
					EntryPrice: activePos.EntryPrice,
					ExitPrice:  activePos.LiquidationPrice,
					Size:       activePos.Size,
					PnL:        -activePos.Margin,
					EntryTime:  activePos.EntryTime,
					ExitTime:   currCandle.Time,
					ExitReason: "LIQUIDATION",
				})
				activePos = nil
				continue
			}
		}

		// 1. Check if Stop Loss or Take Profit triggered on the current candle (using High/Low)
		if activePos != nil {
			triggered := false
			triggerPrice := 0.0
			reason := ""

			if activePos.Side == "LONG" {
				if activePos.StopLossPrice > 0 && currCandle.Low <= activePos.StopLossPrice {
					triggered = true
					triggerPrice = activePos.StopLossPrice
					reason = "SL"
				} else if activePos.TakeProfitPrice > 0 && currCandle.High >= activePos.TakeProfitPrice {
					triggered = true
					triggerPrice = activePos.TakeProfitPrice
					reason = "TP"
				}
			} else if activePos.Side == "SHORT" {
				if activePos.StopLossPrice > 0 && currCandle.High >= activePos.StopLossPrice {
					triggered = true
					triggerPrice = activePos.StopLossPrice
					reason = "SL"
				} else if activePos.TakeProfitPrice > 0 && currCandle.Low <= activePos.TakeProfitPrice {
					triggered = true
					triggerPrice = activePos.TakeProfitPrice
					reason = "TP"
				}
			}

			if triggered {
				// Close position
				var pnl float64
				if activePos.Side == "LONG" {
					pnl = (triggerPrice - activePos.EntryPrice) * activePos.Size
				} else {
					pnl = (activePos.EntryPrice - triggerPrice) * activePos.Size
				}

				// Apply exit fee
				exitFee := triggerPrice * activePos.Size * feeRate
				pnl -= exitFee

				balance += pnl

				completedTrades = append(completedTrades, BacktestTrade{
					Symbol:     symbol,
					Side:       activePos.Side,
					EntryPrice: activePos.EntryPrice,
					ExitPrice:  triggerPrice,
					Size:       activePos.Size,
					PnL:        pnl,
					EntryTime:  activePos.EntryTime,
					ExitTime:   currCandle.Time,
					ExitReason: reason,
				})

				activePos = nil
				continue // Move to next candle
			}
		}

		// 2. Evaluate Strategy Decision
		history := candles[:i+1]
		dec, err := strat.Evaluate(symbol, history)
		if err != nil {
			return nil, err
		}

		if dec.Decision == strategy.HOLD {
			continue
		}

		// If strategy decides CLOSE, close position if active
		if dec.Decision == strategy.CLOSE {
			if activePos != nil {
				var pnl float64
				if activePos.Side == "LONG" {
					pnl = (currClose - activePos.EntryPrice) * activePos.Size
				} else {
					pnl = (activePos.EntryPrice - currClose) * activePos.Size
				}

				exitFee := currClose * activePos.Size * feeRate
				pnl -= exitFee

				balance += pnl

				completedTrades = append(completedTrades, BacktestTrade{
					Symbol:     symbol,
					Side:       activePos.Side,
					EntryPrice: activePos.EntryPrice,
					ExitPrice:  currClose,
					Size:       activePos.Size,
					PnL:        pnl,
					EntryTime:  activePos.EntryTime,
					ExitTime:   currCandle.Time,
					ExitReason: "CLOSE",
				})

				activePos = nil
			}
			continue
		}

		// If strategy decides LONG or SHORT:
		// Check if opposite position is active and close it first
		if activePos != nil && ((dec.Decision == strategy.LONG && activePos.Side == "SHORT") || (dec.Decision == strategy.SHORT && activePos.Side == "LONG")) {
			var pnl float64
			if activePos.Side == "LONG" {
				pnl = (currClose - activePos.EntryPrice) * activePos.Size
			} else {
				pnl = (activePos.EntryPrice - currClose) * activePos.Size
			}

			exitFee := currClose * activePos.Size * feeRate
			pnl -= exitFee

			balance += pnl

			completedTrades = append(completedTrades, BacktestTrade{
				Symbol:     symbol,
				Side:       activePos.Side,
				EntryPrice: activePos.EntryPrice,
				ExitPrice:  currClose,
				Size:       activePos.Size,
				PnL:        pnl,
				EntryTime:  activePos.EntryTime,
				ExitTime:   currCandle.Time,
				ExitReason: "SWITCH",
			})

			activePos = nil
		}

		// Open new position if none active
		if activePos == nil {
			targetLev := dec.Leverage
			if targetLev > leverage {
				targetLev = leverage
			}

			// Calculate StopLoss/TakeProfit targets BEFORE sizing — risk-based sizing
			// needs the stop distance.
			var slPrice, tpPrice float64
			if dec.Decision == strategy.LONG {
				slPrice = currClose * (1.0 - (dec.StopLossPct / 100.0))
				tpPrice = currClose * (1.0 + (dec.TakeProfitPct / 100.0))
			} else { // SHORT
				slPrice = currClose * (1.0 + (dec.StopLossPct / 100.0))
				tpPrice = currClose * (1.0 - (dec.TakeProfitPct / 100.0))
			}

			// Risk-based sizing: a stop-out loses ~riskPct% of balance, independent of
			// leverage. Mirrors the live engine exactly (same shared helper).
			slDist := strategy.SLDistance(currClose, slPrice)
			qty := strategy.RiskBasedQty(balance, riskPct, slDist, currClose, targetLev)
			tradeUSDT := qty * currClose

			// Apply entry fee
			entryFee := tradeUSDT * feeRate
			balance -= entryFee

			// Deposited margin = notional / leverage. Liquidation occurs when an adverse
			// move of ~1/leverage wipes that margin (fees/maintenance margin ignored for simplicity).
			margin := tradeUSDT / float64(targetLev)
			var liqPrice float64
			if targetLev > 0 {
				if dec.Decision == strategy.LONG {
					liqPrice = currClose * (1.0 - 1.0/float64(targetLev))
				} else { // SHORT
					liqPrice = currClose * (1.0 + 1.0/float64(targetLev))
				}
			}

			activePos = &simPosition{
				Side:             string(dec.Decision),
				Size:             qty,
				EntryPrice:       currClose,
				StopLossPrice:    slPrice,
				TakeProfitPrice:  tpPrice,
				LiquidationPrice: liqPrice,
				Margin:           margin,
				EntryTime:        currCandle.Time,
			}
		}
	}

	// Force close active position at the end of simulation
	if activePos != nil {
		finalCandle := candles[n-1]
		finalClose := finalCandle.Close

		var pnl float64
		if activePos.Side == "LONG" {
			pnl = (finalClose - activePos.EntryPrice) * activePos.Size
		} else {
			pnl = (activePos.EntryPrice - finalClose) * activePos.Size
		}

		exitFee := finalClose * activePos.Size * feeRate
		pnl -= exitFee

		balance += pnl

		completedTrades = append(completedTrades, BacktestTrade{
			Symbol:     symbol,
			Side:       activePos.Side,
			EntryPrice: activePos.EntryPrice,
			ExitPrice:  finalClose,
			Size:       activePos.Size,
			PnL:        pnl,
			EntryTime:  activePos.EntryTime,
			ExitTime:   finalCandle.Time,
			ExitReason: "FORCE_CLOSE",
		})
	}

	// Calculate Report Metrics
	totalTrades := len(completedTrades)
	wins := 0
	totalProfit := 0.0
	totalLoss := 0.0
	var tradeReturns []float64

	for _, t := range completedTrades {
		tradeReturns = append(tradeReturns, t.PnL)
		if t.PnL > 0 {
			wins++
			totalProfit += t.PnL
		} else {
			totalLoss += math.Abs(t.PnL)
		}
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = (float64(wins) / float64(totalTrades)) * 100.0
	}

	profitFactor := 0.0
	if totalLoss > 0 {
		profitFactor = totalProfit / totalLoss
	} else if totalProfit > 0 {
		profitFactor = 999.9 // No losses
	}

	// Sharpe Ratio calculation based on trade returns deviation
	sharpe := 0.0
	if totalTrades > 1 {
		sumReturns := 0.0
		for _, r := range tradeReturns {
			sumReturns += r
		}
		avgReturn := sumReturns / float64(totalTrades)

		varianceSum := 0.0
		for _, r := range tradeReturns {
			diff := r - avgReturn
			varianceSum += diff * diff
		}
		stdDev := math.Sqrt(varianceSum / float64(totalTrades-1))

		if stdDev > 0 {
			sharpe = avgReturn / stdDev
		}
	}

	totalReturnPct := ((balance - initialBalance) / initialBalance) * 100.0

	return &BacktestReport{
		StrategyName:   strat.Name(),
		Symbol:         symbol,
		InitialBalance: initialBalance,
		FinalBalance:   balance,
		TotalReturnPct: totalReturnPct,
		MaxDrawdownPct: maxDrawdown,
		WinRatePct:     winRate,
		ProfitFactor:   profitFactor,
		SharpeRatio:    sharpe,
		TotalTrades:    totalTrades,
		Trades:         completedTrades,
	}, nil
}
