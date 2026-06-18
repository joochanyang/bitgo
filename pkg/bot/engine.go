package bot

import (
	"fmt"
	"sync"
	"time"

	"go-bot/pkg/ai"
	"go-bot/pkg/config"
	"go-bot/pkg/db"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// Engine orchestrates the trading loop and state
type Engine struct {
	mu         sync.Mutex
	Exchange   exchange.Exchange
	AIClient   *ai.AIClient
	IsRunning  bool
	Config     *config.Config
	stopChan   chan struct{}
	onTick     func() // WebSocket broadcast callback
	Strategies map[string]strategy.Strategy
	wsTestnet  bool // public WS price feed uses testnet when true (default false = mainnet)

	// marketViews holds the latest per-symbol snapshot the dashboard explains in plain
	// Korean. Guarded by mu (written each tick, read by MarketViews()).
	marketViews map[string]MarketView
}

// NewEngine creates a new trading bot engine
func NewEngine(ex exchange.Exchange, aiClient *ai.AIClient, onTickCallback func()) *Engine {
	trendFollow := strategy.NewTrendFollowing()
	meanRevert := strategy.NewMeanReversion()
	volBreakout := strategy.NewVolatilityBreakout()
	aiStrat := strategy.NewAIStrategy(ex, aiClient)

	return &Engine{
		Exchange:  ex,
		AIClient:  aiClient,
		IsRunning: false,
		Config:    config.GetConfig(),
		stopChan:  make(chan struct{}),
		onTick:    onTickCallback,
		Strategies: map[string]strategy.Strategy{
			"trend_following":     trendFollow,
			"mean_reversion":      meanRevert,
			"volatility_breakout": volBreakout,
			"ai":                  aiStrat,
		},
		marketViews: make(map[string]MarketView),
	}
}

// config returns the engine's current config snapshot under the lock (race-free read).
func (e *Engine) config() *config.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Config
}

// exchange returns the active exchange under the lock (race-free read against SetExchange).
func (e *Engine) exchange() exchange.Exchange {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Exchange
}

// ActiveExchange returns the current exchange backend (race-free read for callers outside the package).
func (e *Engine) ActiveExchange() exchange.Exchange {
	return e.exchange()
}

// Running reports whether the trading loop is active (race-free read).
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.IsRunning
}

// recordMarketView stores the latest snapshot for a symbol (race-free write).
func (e *Engine) recordMarketView(mv MarketView) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.marketViews[mv.Symbol] = mv
}

// MarketViews returns a copy of the latest per-symbol snapshots, each rendered into a
// plain-Korean Situation for the dashboard. IsRunning is overridden with the live engine
// state so a stopped bot reads as "stopped" even if the last tick recorded it running.
func (e *Engine) MarketViews() map[string]SymbolStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]SymbolStatus, len(e.marketViews))
	for sym, mv := range e.marketViews {
		mv.IsRunning = e.IsRunning
		out[sym] = SymbolStatus{View: mv, Situation: describeSituation(mv)}
	}
	return out
}

// Start launches the background trading scheduler loop
func (e *Engine) Start() {
	e.mu.Lock()
	if e.IsRunning {
		e.mu.Unlock()
		return
	}
	e.IsRunning = true
	e.stopChan = make(chan struct{})
	e.mu.Unlock()

	cfg := e.config()
	db.LogInfo("Trading engine started. Mode: Paper Trading = %t, AI Provider = %s", cfg.IsPaperTrading, cfg.AIProvider)

	// Run initial tick immediately
	go e.RunTick()

	// Capture the stop channel for THIS run so a later Start() reassigning e.stopChan
	// cannot leave this goroutine selecting on the wrong channel.
	e.mu.Lock()
	stopChan := e.stopChan
	e.mu.Unlock()

	// Real-time trailing-stop monitor: streams public prices and tightens stops
	// between ticks. Uses the same captured stopChan so a later Start() can't strand it.
	go e.runStopMonitor(stopChan)

	// Parse interval duration
	duration := e.parseInterval(cfg.Interval)
	ticker := time.NewTicker(duration)

	go func() {
		for {
			select {
			case <-ticker.C:
				e.RunTick()
				// Dynamically adjust ticker if config changed
				newDuration := e.parseInterval(config.GetConfig().Interval)
				if newDuration != duration {
					duration = newDuration
					ticker.Reset(duration)
				}
			case <-stopChan:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop pauses the trading engine
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.IsRunning {
		return
	}
	e.IsRunning = false
	close(e.stopChan)
	db.LogInfo("Trading engine stopped/paused.")
	if e.onTick != nil {
		e.onTick()
	}
}

// RunTick executes a single evaluation tick for all configured symbols
func (e *Engine) RunTick() {
	e.mu.Lock()
	if !e.IsRunning {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	db.LogInfo("Starting market analysis tick...")

	// Refresh config snapshot under the lock (race-free with concurrent readers).
	cfg := config.GetConfig()
	e.mu.Lock()
	e.Config = cfg
	e.mu.Unlock()

	// Synchronize local trade history with exchange positions
	e.syncTradeHistory()

	for _, symbol := range cfg.Symbols {
		db.LogInfo("Analyzing symbol: %s", symbol)
		err := e.analyzeAndTrade(symbol)
		if err != nil {
			db.LogError("Error during analysis for %s: %v", symbol, err)
		}
	}

	db.LogInfo("Market analysis tick completed.")
	if e.onTick != nil {
		e.onTick() // Broadcast status updates to websocket clients
	}
}

// SetExchange updates the active exchange instance (e.g. mock -> live)
func (e *Engine) SetExchange(ex exchange.Exchange) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Exchange = ex
	// Rebind any strategy that holds its own exchange reference, so it reads
	// balance/position from the same backend that orders execute against.
	if aiStrat, ok := e.Strategies["ai"].(*strategy.AIStrategy); ok {
		aiStrat.SetExchange(ex)
	}
	db.LogInfo("Exchange backend switched. Paper trading: %t", e.Config.IsPaperTrading)
}

func (e *Engine) analyzeAndTrade(symbol string) error {
	// Capture the backend and config once for the whole tick so a concurrent
	// SetExchange/config update cannot make us read from one backend and trade on another.
	ex := e.exchange()
	cfg := e.config()

	// 1. Fetch candles
	candles, err := ex.GetKlines(symbol, cfg.Interval, 200)
	if err != nil {
		return fmt.Errorf("failed to fetch candles: %v", err)
	}

	if len(candles) < 200 {
		return fmt.Errorf("insufficient candles fetched: got %d, need 200", len(candles))
	}

	// 2. Fetch current position & balance
	position, err := ex.GetPosition(symbol)
	if err != nil {
		return fmt.Errorf("failed to fetch position: %v", err)
	}

	balance, err := ex.GetBalance()
	if err != nil {
		return fmt.Errorf("failed to fetch balance: %v", err)
	}

	// 3. Execute Trailing Stop Loss logic if position is active
	currentPrice := candles[len(candles)-1].Close
	if position.Side != "NONE" && position.Size > 0 {
		e.updateTrailingStop(ex, symbol, position, currentPrice)
		// Re-fetch position in case trailing stop logic modified it
		if posUpdated, err := ex.GetPosition(symbol); err == nil {
			position = posUpdated
		}
	}

	// 4. Retrieve Active Strategy
	stratName := cfg.ActiveStrategy
	if stratName == "" {
		stratName = "trend_following"
	}
	strat, ok := e.Strategies[stratName]
	if !ok {
		db.LogWarn("Configured strategy %s not found. Falling back to trend_following.", stratName)
		strat = e.Strategies["trend_following"]
	}

	// 5. Evaluate Strategy
	db.LogInfo("Evaluating strategy '%s' for %s...", strat.Name(), symbol)
	decision, err := strat.Evaluate(symbol, candles)
	if err != nil {
		return fmt.Errorf("strategy evaluation failed: %v", err)
	}

	db.LogInfo("Strategy Decision for %s: %s (Confidence: %.2f, Leverage: %dx)",
		symbol, decision.Decision, decision.Confidence, decision.Leverage)
	db.LogInfo("Reasoning: %s", decision.Reasoning)

	// Record a plain-Korean-ready snapshot for the dashboard's "current situation" card.
	hasPos := position.Side != "NONE" && position.Size > 0
	mv := MarketView{
		Symbol:        symbol,
		IsRunning:     true, // we only reach here while running
		HasPosition:   hasPos,
		Side:          position.Side,
		Decision:      string(decision.Decision),
		CurrentPrice:  currentPrice,
		Reasoning:     decision.Reasoning,
		UnrealizedPnL: position.UnrealizedPnL,
	}
	if hasPos {
		mv.EntryPrice = position.EntryPrice
		mv.StopLossPrice = position.StopLossPrice
		mv.TakeProfitPct = position.TakeProfitPrice
	}
	e.recordMarketView(mv)

	// 6. Execute Order based on decision
	return e.executeDecision(ex, cfg, symbol, decision, position, balance, currentPrice)
}

func (e *Engine) executeDecision(ex exchange.Exchange, cfg *config.Config, symbol string, decision *strategy.Decision, pos *exchange.Position, balance float64, currentPrice float64) error {
	// If strategy suggests HOLD, do nothing
	if decision.Decision == strategy.HOLD {
		db.LogInfo("Decision is HOLD for %s. No action taken.", symbol)
		return nil
	}

	// If strategy suggests CLOSE, close active position if exists
	if decision.Decision == strategy.CLOSE {
		if pos.Side != "NONE" && pos.Size > 0 {
			db.LogInfo("Closing active %s position for %s...", pos.Side, symbol)
			err := ex.ClosePosition(symbol)
			if err != nil {
				return fmt.Errorf("failed to close position: %v", err)
			}

			// Record closed trade
			tradeRecord := db.Trade{
				ID:          fmt.Sprintf("trade-%d", time.Now().UnixNano()),
				Symbol:      symbol,
				Side:        pos.Side,
				Size:        pos.Size,
				EntryPrice:  pos.EntryPrice,
				ExitPrice:   currentPrice,
				RealizedPnL: pos.UnrealizedPnL,
				Leverage:    pos.Leverage,
				Timestamp:   time.Now(),
				IsPaper:     cfg.IsPaperTrading,
				Status:      "CLOSED",
			}
			db.UpdateTrade(tradeRecord)
			db.LogInfo("Closed %s position. PnL: %.2f USDT", symbol, pos.UnrealizedPnL)
		} else {
			db.LogInfo("Close order suggested, but no active position exists for %s.", symbol)
		}
		return nil
	}

	// For LONG/SHORT, check if we need to close an opposite position first
	if (decision.Decision == strategy.LONG && pos.Side == "SHORT") || (decision.Decision == strategy.SHORT && pos.Side == "LONG") {
		db.LogInfo("Opposite position exists for %s. Closing existing %s position first...", symbol, pos.Side)
		err := ex.ClosePosition(symbol)
		if err != nil {
			return fmt.Errorf("failed to close opposite position: %v", err)
		}
		// Record closed trade
		tradeRecord := db.Trade{
			ID:          fmt.Sprintf("trade-%d", time.Now().UnixNano()),
			Symbol:      symbol,
			Side:        pos.Side,
			Size:        pos.Size,
			EntryPrice:  pos.EntryPrice,
			ExitPrice:   currentPrice,
			RealizedPnL: pos.UnrealizedPnL,
			Leverage:    pos.Leverage,
			Timestamp:   time.Now(),
			IsPaper:     cfg.IsPaperTrading,
			Status:      "CLOSED",
		}
		db.UpdateTrade(tradeRecord)

		// Reset position locally so subsequent code treats it as NONE
		pos = &exchange.Position{Symbol: symbol, Side: "NONE", Size: 0}
		// Fetch fresh balance since profit/loss is realized
		freshBalance, err := ex.GetBalance()
		if err == nil {
			balance = freshBalance
		}
	}

	// Only open new position if we don't already have one (simplification)
	if pos.Side == "NONE" || pos.Size == 0 {
		// Calculate position size based on risk and balance
		// Leverage capped by config and strategy recommendations
		targetLeverage := decision.Leverage
		if targetLeverage > cfg.Leverage {
			targetLeverage = cfg.Leverage
		}

		// Set StopLoss/TakeProfit prices BEFORE sizing — risk-based sizing needs the
		// stop distance.
		var slPrice, tpPrice float64
		if decision.Decision == strategy.LONG {
			slPrice = currentPrice * (1.0 - (decision.StopLossPct / 100.0))
			tpPrice = currentPrice * (1.0 + (decision.TakeProfitPct / 100.0))
		} else { // SHORT
			slPrice = currentPrice * (1.0 + (decision.StopLossPct / 100.0))
			tpPrice = currentPrice * (1.0 - (decision.TakeProfitPct / 100.0))
		}

		// Portfolio risk cap: reduce this entry's risk so the TOTAL risk across all
		// open positions stays within MaxPortfolioRiskPct of balance. Without this,
		// N symbols signalling at once would each risk a full RiskPercentage,
		// stacking to N x the intended portfolio risk.
		committedRisk := e.committedPortfolioRisk(ex, cfg.Symbols, symbol)
		riskPct := strategy.AvailableRiskPct(balance, cfg.MaxPortfolioRiskPct, committedRisk, cfg.RiskPercentage)
		if riskPct <= 0 {
			db.LogInfo("Skipping %s entry: portfolio risk budget (%.1f%%) exhausted by open positions.",
				symbol, cfg.MaxPortfolioRiskPct)
			return nil
		}

		// Risk-based sizing: a stop-out loses ~riskPct% of balance, independent
		// of leverage. Leverage (set below) governs margin/liquidation, not size.
		slDist := strategy.SLDistance(currentPrice, slPrice)
		qty := strategy.RiskBasedQty(balance, riskPct, slDist, currentPrice, targetLeverage)

		sideString := "Buy"
		if decision.Decision == strategy.SHORT {
			sideString = "Sell"
		}

		db.LogInfo("Opening %s position on %s. Size: %.3f Qty, Lev: %dx, SL: %.4f, TP: %.4f",
			decision.Decision, symbol, qty, targetLeverage, slPrice, tpPrice)

		// Set leverage on the exchange BEFORE placing the order so the position's margin
		// requirement matches. With risk-based sizing, leverage governs margin/liquidation,
		// not position size; without setting it, live fills use the account default.
		if err := ex.SetLeverage(symbol, targetLeverage); err != nil {
			return fmt.Errorf("failed to set leverage to %dx for %s: %v", targetLeverage, symbol, err)
		}

		opts := exchange.OrderOptions{
			StopLossPrice:   slPrice,
			TakeProfitPrice: tpPrice,
		}

		res, err := ex.PlaceOrder(symbol, sideString, qty, 0, opts)
		if err != nil {
			return fmt.Errorf("failed to place order: %v", err)
		}

		// Use the actual fill price reported by the exchange; fall back to the
		// signal-bar close only if the backend could not report a fill price.
		entryPrice := currentPrice
		if res.Price > 0 {
			entryPrice = res.Price
		}

		// Record open trade
		tradeRecord := db.Trade{
			ID:         res.OrderID,
			Symbol:     symbol,
			Side:       string(decision.Decision),
			Size:       qty,
			EntryPrice: entryPrice,
			Leverage:   targetLeverage,
			Timestamp:  time.Now(),
			IsPaper:    cfg.IsPaperTrading,
			Status:     "OPEN",
		}
		db.AddTrade(tradeRecord)
		db.LogInfo("Successfully opened %s position for %s. OrderID: %s", decision.Decision, symbol, res.OrderID)
	} else {
		db.LogInfo("Position already active for %s (%s %.3f). Maintaining position.", symbol, pos.Side, pos.Size)
	}

	return nil
}

// committedPortfolioRisk sums the at-stop loss of every OTHER open position (in quote
// currency), so a new entry can be sized against the remaining portfolio risk budget.
// excludeSymbol is the symbol being entered now (its own position, if any, is skipped).
func (e *Engine) committedPortfolioRisk(ex exchange.Exchange, symbols []string, excludeSymbol string) float64 {
	total := 0.0
	for _, sym := range symbols {
		if sym == excludeSymbol {
			continue
		}
		pos, err := ex.GetPosition(sym)
		if err != nil || pos == nil || pos.Side == "NONE" || pos.Size == 0 {
			continue
		}
		total += strategy.PositionRiskUSDT(pos.Size, pos.EntryPrice, pos.StopLossPrice)
	}
	return total
}

// updateTrailingStop adjusts the stop loss level dynamically to lock in profit
func (e *Engine) updateTrailingStop(ex exchange.Exchange, symbol string, pos *exchange.Position, currentPrice float64) {
	newSL, shouldUpdate := trailingStopTarget(pos.Side, pos.EntryPrice, pos.StopLossPrice, currentPrice)
	if !shouldUpdate {
		return
	}
	db.LogInfo("[TRAILING STOP] Moving %s stop loss for %s from %.4f to %.4f (Current Price: %.4f)",
		pos.Side, symbol, pos.StopLossPrice, newSL, currentPrice)
	if err := ex.SetStopLoss(symbol, newSL); err != nil {
		db.LogError("[TRAILING STOP] Failed to update stop loss: %v", err)
	}
}

// runStopMonitor streams real-time public prices and tightens trailing stops between
// ticks. It blocks until stop is closed (per the Start() captured-stopChan pattern, so
// a later Start() cannot strand it).
//
// SAFETY: the server-side hard stop-loss placed at entry protects the position during
// any WS gap; a dropped feed only pauses trailing tightening, never protection.
func (e *Engine) runStopMonitor(stop <-chan struct{}) {
	// lastSL is owned solely by this goroutine (no lock); it skips redundant SetStopLoss
	// REST calls when a fast favorable move would otherwise re-send the same target.
	lastSL := make(map[string]float64)

	ws := exchange.NewPublicWS(e.wsTestnet, func(symbol string, price float64) {
		ex := e.exchange() // fresh backend each update (mock/live swap-safe)
		pos, err := ex.GetPosition(symbol)
		if err != nil || pos == nil || pos.Side == "NONE" || pos.Size == 0 {
			return
		}
		newSL, shouldUpdate := trailingStopTarget(pos.Side, pos.EntryPrice, pos.StopLossPrice, price)
		if !shouldUpdate || lastSL[symbol] == newSL {
			return
		}
		if err := ex.SetStopLoss(symbol, newSL); err != nil {
			db.LogError("[WS TRAILING] SetStopLoss failed for %s: %v", symbol, err)
			return
		}
		lastSL[symbol] = newSL
		db.LogInfo("[WS TRAILING] %s %s stop loss -> %.4f @ price %.4f", symbol, pos.Side, newSL, price)
	})

	ws.Run(stop, e.config().Symbols)
}

func (e *Engine) parseInterval(interval string) time.Duration {
	switch interval {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return 1 * time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		return 1 * time.Hour
	}
}

// syncTradeHistory synchronizes local trades database with exchange positions
func (e *Engine) syncTradeHistory() {
	ex := e.exchange()
	trades, err := db.GetTrades()
	if err != nil {
		db.LogError("Failed to load trades for sync: %v", err)
		return
	}

	for _, t := range trades {
		if t.Status != "OPEN" {
			continue
		}

		pos, err := ex.GetPosition(t.Symbol)
		if err != nil {
			// Skip to avoid closing trades on API errors
			continue
		}

		// If position is closed on exchange, or the side changed, synchronize database record
		if pos.Side == "NONE" || pos.Side != t.Side || pos.Size == 0 {
			currentPrice, err := ex.GetTicker(t.Symbol)
			if err != nil {
				if pos.MarkPrice > 0 {
					currentPrice = pos.MarkPrice
				} else {
					currentPrice = t.EntryPrice // absolute fallback
				}
			}

			// Calculate realized PnL
			var pnl float64
			if t.Side == "LONG" {
				pnl = (currentPrice - t.EntryPrice) * t.Size
			} else { // SHORT
				pnl = (t.EntryPrice - currentPrice) * t.Size
			}

			t.Status = "CLOSED"
			t.ExitPrice = currentPrice
			t.RealizedPnL = pnl
			t.Timestamp = time.Now()

			err = db.UpdateTrade(t)
			if err != nil {
				db.LogError("Failed to update synced trade in database: %v", err)
			} else {
				db.LogInfo("Sync: Detected closed position for %s. Database record updated. Realized PnL: %.2f USDT", t.Symbol, pnl)
			}
		}
	}
}
