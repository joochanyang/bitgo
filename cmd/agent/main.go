package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
	"go-bot/pkg/agent/guard"
	"go-bot/pkg/agent/memory"
	"go-bot/pkg/agent/runner"
	"go-bot/pkg/config"
	"go-bot/pkg/exchange"
)

// agentMinConfidence is the guard's confidence floor for the AI agent. Below this the
// guard downgrades entries to HOLD. Conservative for paper validation.
const agentMinConfidence = 0.55

func main() {
	_ = godotenv.Load() // best-effort: .env for keys (BYBIT_*, DEEPSEEK_*)

	cfg := config.GetConfig()

	ex := exchange.NewBybitExchange(cfg.BybitAPIKey, cfg.BybitAPISecret, false)

	council, label := pickCouncil(os.Getenv)
	mem := memory.New("agent_memory.json")
	g := guard.New(agentMinConfidence)

	r := &runner.Runner{
		Council: council,
		Guard:   g,
		Execute: func(sd agent.SafeDecision) error {
			d := sd.Decision()
			log.Printf("[PAPER ENTRY] %s size=%.2f%% SL=%.2f%% TP=%.2f%% conf=%.2f reason=%q",
				d.Action, d.SizePct, d.StopLossPct, d.TakeProfitPct, d.Confidence, d.Reasoning)
			return nil // paper: no order placed
		},
		Record: func(ep agent.TradeEpisode) error {
			ep.OpenedAt = time.Now()
			return mem.Record(ep)
		},
		OnReject: func(rs []agent.Rejection) {
			for _, rj := range rs {
				log.Printf("[GUARD] %s: %s", rj.Rule, rj.Message)
			}
		},
		NowNano: func() int64 { return time.Now().UnixNano() },
		Nonce:   nonce,
	}

	period := tickInterval(cfg.Interval)
	log.Printf("AI agent starting (PAPER) — council=%s symbols=%v interval=%s(%s) risk=%.1f%% lev=%d",
		label, cfg.Symbols, cfg.Interval, period, cfg.RiskPercentage, cfg.Leverage)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runCycle(r, ex, cfg, mem) // run once immediately, then on the ticker
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Println("AI agent stopping (signal received)")
			return
		case <-ticker.C:
			runCycle(r, ex, cfg, mem)
		}
	}
}

// runCycle runs one tick across all configured symbols. A per-symbol error is logged and
// skipped so other symbols and later ticks continue (error isolation).
func runCycle(r *runner.Runner, ex exchange.Exchange, cfg *config.Config, mem *memory.Store) {
	for _, sym := range cfg.Symbols {
		bctx, err := buildContext(ex, sym, cfg.Interval, mem, 5)
		if err != nil {
			log.Printf("[%s] context error: %v (skipping)", sym, err)
			continue
		}
		// Close any open episodes the current price would have stopped out / taken profit,
		// BEFORE the council recalls — so it learns from fresh outcomes this same tick.
		retrospect(mem, sym, bctx.Price, time.Now())
		acc, err := buildAccount(ex, sym, cfg.Symbols, cfg.Leverage, cfg.MaxPortfolioRiskPct)
		if err != nil {
			log.Printf("[%s] account error: %v (skipping)", sym, err)
			continue
		}
		log.Printf("[%s] regime=%s price=%.6f balanceOK=%v committedRisk=%.2f",
			sym, bctx.Regime, bctx.Price, acc.BalanceOK, acc.CommittedRiskUSDT)
		if err := r.RunOnce(bctx, acc); err != nil {
			log.Printf("[%s] cycle error: %v", sym, err)
		}
	}
}

// nonce returns a short random hex string for episode IDs.
func nonce() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// compile-time: ensure council types satisfy the interface (defensive).
var _ brain.Council = (*brain.MockCouncil)(nil)
