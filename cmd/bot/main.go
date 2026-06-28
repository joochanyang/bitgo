package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"go-bot/pkg/ai"
	"go-bot/pkg/bot"
	"go-bot/pkg/config"
	"go-bot/pkg/db"
	"go-bot/pkg/exchange"
	"go-bot/pkg/notify"
	"go-bot/pkg/web"
)

func main() {
	// Parse optional flags
	envPath := flag.String("env", ".env", "Path to .env file")
	flag.Parse()

	// Load environment variables if .env file exists
	if _, err := os.Stat(*envPath); err == nil {
		if err := godotenv.Load(*envPath); err != nil {
			log.Printf("Warning: Failed to load .env file: %v", err)
		}
	}

	// 1. Load System Configuration
	cfg := config.GetConfig()
	db.LogInfo("Config loaded successfully. Server Port: %s, Selected AI: %s", cfg.ServerPort, cfg.AIProvider)

	// 2. Initialize Exchange Clients
	// We instantiate both Mock (Paper) and Bybit (Live) backends to allow hot-swapping
	mockEx := exchange.NewMockExchange(10000.0) // Starts with 10,000 USDT mock balance

	var liveEx exchange.Exchange
	if cfg.BybitAPIKey != "" && cfg.BybitAPISecret != "" {
		liveEx = exchange.NewBybitExchange(cfg.BybitAPIKey, cfg.BybitAPISecret, false)
		db.LogInfo("Real Bybit Exchange client initialized.")
	} else {
		db.LogWarn("Bybit API credentials not set. Live trading will be unavailable.")
	}

	// Determine active exchange
	var activeEx exchange.Exchange
	if cfg.IsPaperTrading {
		activeEx = mockEx
		db.LogInfo("Paper Trading (Mock) selected as the active exchange backend.")
	} else {
		if liveEx == nil {
			db.LogError("Live trading enabled but API keys are missing. Falling back to Paper Trading.")
			if err := config.SetPaperTrading(true); err != nil {
				db.LogError("Failed to persist paper-trading fallback: %v", err)
			}
			activeEx = mockEx
		} else {
			activeEx = liveEx
			db.LogInfo("LIVE TRADING active. Orders will execute on real market.")
		}
	}

	// 3. Initialize AI Client
	aiClient := ai.NewAIClient()

	// 4. Initialize WebServer and Engine Callbacks
	var engine *bot.Engine
	var webServer *web.WebServer

	// Broadcast callback to notify WS clients when engine ticks
	onTickBroadcast := func() {
		if webServer != nil {
			webServer.BroadcastState()
		}
	}

	// Engine instantiation
	engine = bot.NewEngine(activeEx, aiClient, onTickBroadcast)

	// Telegram trade alerts (open/close/error). Disabled (no-op) when either env
	// var is empty, so the bot runs fine without Telegram configured.
	tgNotifier := notify.New(os.Getenv("TELEGRAM_BOT_TOKEN"), os.Getenv("TELEGRAM_CHAT_ID"))
	engine.SetNotifier(tgNotifier)
	if tgNotifier != nil {
		db.LogInfo("Telegram notifications enabled.")
	} else {
		db.LogInfo("Telegram notifications disabled (TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID not set).")
	}

	// WebServer instantiation
	engineStateCb := func() (bool, bool, exchange.Exchange) {
		return engine.Running(), config.GetConfig().IsPaperTrading, engine.ActiveExchange()
	}

	triggerTickCb := func() {
		if !engine.IsRunning {
			engine.Start()
		} else {
			engine.RunTick()
		}
	}

	stopBotCb := func() {
		engine.Stop()
	}

	// setPaperModeCb only swaps the active backend; the IsPaperTrading flag itself is
	// persisted by the web handler via config.UpdateConfig before this is called.
	setPaperModeCb := func(isPaper bool) {
		if isPaper {
			engine.SetExchange(mockEx)
			return
		}
		// Live mode: lazily initialize the live client if credentials were entered
		// dynamically via the UI. Refuse the switch when keys are still missing so
		// we never install a BybitExchange built from empty credentials (its REST
		// calls would fail on every tick with confusing auth errors).
		if liveEx == nil {
			live := config.GetConfig()
			if live.BybitAPIKey == "" || live.BybitAPISecret == "" {
				db.LogError("Live 전환 불가: Bybit API 키가 설정되지 않았습니다. Paper 모드를 유지합니다. " +
					"대시보드 또는 .env에 bybit_api_key/bybit_api_secret(또는 BYBIT_API_KEY/BYBIT_API_SECRET)를 입력하세요.")
				engine.SetExchange(mockEx)
				return
			}
			liveEx = exchange.NewBybitExchange(live.BybitAPIKey, live.BybitAPISecret, false)
			db.LogInfo("Real Bybit Exchange client initialized (live switch).")
		}
		engine.SetExchange(liveEx)
	}

	closePosCb := func(symbol string) error {
		return engine.ActiveExchange().ClosePosition(symbol)
	}

	webServer = web.NewWebServer(engineStateCb, triggerTickCb, stopBotCb, setPaperModeCb, closePosCb, engine.Strategies)

	// Surface the engine's per-symbol plain-Korean status to the dashboard's situation card.
	webServer.SetSituationProvider(engine.MarketViews)

	// Surface the next scheduled tick so the dashboard can show a "waiting for next
	// analysis" countdown while the engine runs quietly between ticks.
	webServer.SetNextTickProvider(engine.NextTickAt)

	// On-demand AI narration: build a compact prompt from the latest per-symbol snapshot
	// and ask the LLM to explain it in plain Korean. Reuses the configured AI provider/key.
	webServer.SetExplainProvider(func() (string, error) {
		views := engine.MarketViews()
		if len(views) == 0 {
			return "아직 분석 데이터가 없습니다. 봇을 시작하면 시세를 분석한 뒤 해설할 수 있어요.", nil
		}
		var b strings.Builder
		for _, st := range views {
			v := st.View
			b.WriteString(fmt.Sprintf("심볼 %s | 봇실행=%t | 포지션보유=%t(%s) | 현재가=%.4f | 진입가=%.4f | 손절=%.4f | 익절=%.4f | 미실현손익=%.2f | 전략판단=%s | 근거=%s\n",
				v.Symbol, v.IsRunning, v.HasPosition, v.Side, v.CurrentPrice, v.EntryPrice, v.StopLossPrice, v.TakeProfitPct, v.UnrealizedPnL, v.Decision, v.Reasoning))
		}
		return aiClient.Explain(b.String())
	})

	// Start bot automatically if configured to run (defaults to stopped for safety)
	// engine.Start() // Uncomment if you want to start automatically on boot

	// 5. Start Web Server
	err := webServer.Start(cfg.ServerPort)
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("Critical: Web server failed: %v", err)
	}
}
