package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go-bot/pkg/backtest"
	"go-bot/pkg/bot"
	"go-bot/pkg/config"
	"go-bot/pkg/db"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// StaticAssets holds the embedded web resources (HTML, CSS, JS)
//
//go:embed static/*
var StaticAssets embed.FS

// Backtest tuning defaults and validation bounds. These are the single source of
// truth for both /api/backtest and /api/backtest/batch.
const (
	defaultBacktestBalance = 10000.0
	defaultBacktestFeeRate = 0.0006
	defaultBacktestCandles = 500

	minBacktestCandles = 51   // warmup is 50; need >=1 tradeable bar
	maxBacktestCandles = 5000 // paged fetch cap (GetKlinesPaged walks 1000/request)
	maxBacktestFeeRate = 0.01 // 1% per side; anything higher is a typo, reject
)

// resolveBacktestParams applies the omitted/zero fallback rules for the optional
// tuning params and validates them. Returns an error message (for HTTP 400) when
// a value is out of range. The asymmetry is load-bearing: a 0 balance or 0 candle
// count means "omitted" (never meaningful), but a 0 fee_rate is a valid zero-fee sim.
func resolveBacktestParams(balance, feeRate *float64, candles *int) (float64, float64, int, string) {
	initialBalance := defaultBacktestBalance
	if balance != nil && *balance != 0 {
		if *balance < 0 {
			return 0, 0, 0, "초기 자본금은 0보다 커야 합니다"
		}
		initialBalance = *balance
	}

	fee := defaultBacktestFeeRate
	if feeRate != nil {
		if *feeRate < 0 || *feeRate > maxBacktestFeeRate {
			return 0, 0, 0, fmt.Sprintf("수수료율은 0에서 %g 사이여야 합니다", maxBacktestFeeRate)
		}
		fee = *feeRate
	}

	candleLimit := defaultBacktestCandles
	if candles != nil && *candles != 0 {
		if *candles < minBacktestCandles || *candles > maxBacktestCandles {
			return 0, 0, 0, fmt.Sprintf("캔들 수는 %d에서 %d 사이여야 합니다", minBacktestCandles, maxBacktestCandles)
		}
		candleLimit = *candles
	}

	return initialBalance, fee, candleLimit, ""
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connection from local dashboard
	},
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type WebServer struct {
	mu           sync.Mutex
	clients      map[*Client]bool
	register     chan *Client
	unregister   chan *Client
	engineState  func() (bool, bool, exchange.Exchange) // Callback to get (isRunning, isPaper, exchangeInstance)
	triggerTick  func()                                 // Callback to force a trade evaluation tick
	stopBot      func()                                 // Callback to stop/pause the trading engine
	setPaperMode func(bool)                             // Callback to change paper trading state
	closePos     func(symbol string) error              // Callback to close position manually
	strategies   map[string]strategy.Strategy           // Strategy engines map
	situations   func() map[string]bot.SymbolStatus     // Optional: per-symbol plain-Korean status (nil = none)
	explain      func() (string, error)                 // Optional: on-demand AI narration (nil = disabled)
	nextTickAt   func() time.Time                       // Optional: next scheduled tick (zero time when stopped)
	authToken    string                                 // If set, required on /api/* and /ws (DASHBOARD_TOKEN)
}

// SetSituationProvider wires the engine's per-symbol status snapshot into the dashboard
// state. Optional: when unset, SystemState.Situations is simply omitted.
func (ws *WebServer) SetSituationProvider(fn func() map[string]bot.SymbolStatus) {
	ws.situations = fn
}

// SetExplainProvider wires the on-demand AI narration used by POST /api/explain.
// Optional: when unset, the endpoint reports that AI explanation is unavailable.
func (ws *WebServer) SetExplainProvider(fn func() (string, error)) {
	ws.explain = fn
}

// SetNextTickProvider wires the engine's next-scheduled-tick time so the dashboard
// can render a "다음 분석까지 N분 남음" countdown. Optional: when unset, the countdown
// is omitted.
func (ws *WebServer) SetNextTickProvider(fn func() time.Time) {
	ws.nextTickAt = fn
}

// authorized reports whether a request carries the configured auth token.
// When no token is configured, every request is allowed (dev convenience).
func (ws *WebServer) authorized(r *http.Request) bool {
	if ws.authToken == "" {
		return true
	}
	if r.Header.Get("X-Auth-Token") == ws.authToken {
		return true
	}
	// Allow ?token= for the WebSocket handshake, where browsers can't set headers.
	return r.URL.Query().Get("token") == ws.authToken
}

// requireAuth wraps a handler with the token check.
func (ws *WebServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ws.authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func NewWebServer(
	engineState func() (bool, bool, exchange.Exchange),
	triggerTick func(),
	stopBot func(),
	setPaperMode func(bool),
	closePos func(symbol string) error,
	strategies map[string]strategy.Strategy,
) *WebServer {
	return &WebServer{
		clients:      make(map[*Client]bool),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		engineState:  engineState,
		triggerTick:  triggerTick,
		stopBot:      stopBot,
		setPaperMode: setPaperMode,
		closePos:     closePos,
		strategies:   strategies,
	}
}

// Start runs the HTTP server and WebSocket Hub
func (ws *WebServer) Start(port string) error {
	go ws.runHub()

	ws.authToken = os.Getenv("DASHBOARD_TOKEN")
	if ws.authToken == "" {
		db.LogWarn("DASHBOARD_TOKEN not set: API and WebSocket endpoints are UNAUTHENTICATED. Set DASHBOARD_TOKEN to require an X-Auth-Token header for live-trading safety.")
	} else {
		db.LogInfo("Dashboard authentication enabled (DASHBOARD_TOKEN). Requests require X-Auth-Token.")
	}

	// REST APIs (token-protected when DASHBOARD_TOKEN is set)
	http.HandleFunc("/api/status", ws.requireAuth(ws.handleStatus))
	http.HandleFunc("/api/config", ws.requireAuth(ws.handleConfig))
	http.HandleFunc("/api/trades", ws.requireAuth(ws.handleTrades))
	http.HandleFunc("/api/logs", ws.requireAuth(ws.handleLogs))
	http.HandleFunc("/api/backtest", ws.requireAuth(ws.handleBacktest))
	http.HandleFunc("/api/backtest/batch", ws.requireAuth(ws.handleBacktestBatch))
	http.HandleFunc("/api/explain", ws.requireAuth(ws.handleExplain))

	// WebSocket
	http.HandleFunc("/ws", ws.requireAuth(ws.handleWebSocket))

	// Embedded static files
	staticFS, err := fs.Sub(StaticAssets, "static")
	if err != nil {
		return err
	}
	http.Handle("/", http.FileServer(http.FS(staticFS)))

	db.LogInfo("Web dashboard server listening on http://localhost:%s", port)
	return http.ListenAndServe(":"+port, nil)
}

// BroadcastState sends updated bot metrics to all connected clients
func (ws *WebServer) BroadcastState() {
	stateData, err := ws.getSystemStateJSON()
	if err != nil {
		db.LogError("Failed to serialize system state for WS: %v", err)
		return
	}

	ws.mu.Lock()
	defer ws.mu.Unlock()

	for client := range ws.clients {
		select {
		case client.send <- stateData:
		default:
			close(client.send)
			delete(ws.clients, client)
		}
	}
}

func (ws *WebServer) runHub() {
	for {
		select {
		case client := <-ws.register:
			ws.mu.Lock()
			ws.clients[client] = true
			ws.mu.Unlock()
			// Send initial state immediately
			if stateData, err := ws.getSystemStateJSON(); err == nil {
				client.send <- stateData
			}
		case client := <-ws.unregister:
			ws.mu.Lock()
			if _, ok := ws.clients[client]; ok {
				delete(ws.clients, client)
				close(client.send)
			}
			ws.mu.Unlock()
		}
	}
}

func (ws *WebServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		db.LogError("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{conn: conn, send: make(chan []byte, 256)}
	ws.register <- client

	// Read loop (to detect disconnects)
	go func() {
		defer func() {
			ws.unregister <- client
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()

	// Write loop
	go func() {
		for message := range client.send {
			err := conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				break
			}
		}
	}()
}

// SystemState represents the data broadcasted to the frontend
type SystemState struct {
	IsRunning      bool                `json:"is_running"`
	IsPaperTrading bool                `json:"is_paper_trading"`
	Balance        float64             `json:"balance"`
	Positions      []exchange.Position `json:"positions"`
	Trades         []db.Trade          `json:"trades"`
	Logs           []db.LogEntry       `json:"logs"`
	Config         config.Config       `json:"config"`
	// Situations: per-symbol plain-Korean status for the dashboard's "current situation"
	// card. Omitted when no provider is wired (e.g. in unit tests).
	Situations map[string]bot.SymbolStatus `json:"situations,omitempty"`
	// NextTickAt: ISO-8601 wall-clock time of the next scheduled tick. Empty when the
	// engine is stopped. Drives the dashboard countdown so a quiet running bot reads as
	// "waiting" rather than "broken".
	NextTickAt string `json:"next_tick_at,omitempty"`
}

func (ws *WebServer) getSystemStateJSON() ([]byte, error) {
	isRunning, isPaper, ex := ws.engineState()
	cfg := config.GetConfig()

	// Mask API keys for safety in front-end
	maskedCfg := *cfg
	if maskedCfg.BybitAPIKey != "" {
		if len(maskedCfg.BybitAPIKey) > 4 {
			maskedCfg.BybitAPIKey = "******" + maskedCfg.BybitAPIKey[len(maskedCfg.BybitAPIKey)-4:]
		} else {
			maskedCfg.BybitAPIKey = "******"
		}
	}
	if maskedCfg.BybitAPISecret != "" {
		maskedCfg.BybitAPISecret = "******"
	}
	if maskedCfg.OpenAIAPIKey != "" {
		maskedCfg.OpenAIAPIKey = "******"
	}
	if maskedCfg.GeminiAPIKey != "" {
		maskedCfg.GeminiAPIKey = "******"
	}

	balance, _ := ex.GetBalance()

	// Fetch positions for active target symbols
	var activePositions []exchange.Position
	for _, sym := range cfg.Symbols {
		if pos, err := ex.GetPosition(sym); err == nil && pos.Size > 0 {
			activePositions = append(activePositions, *pos)
		}
	}

	trades, _ := db.GetTrades()
	logs, _ := db.GetLogs()

	// Reverse trades to show newest first
	reversedTrades := make([]db.Trade, len(trades))
	for i, t := range trades {
		reversedTrades[len(trades)-1-i] = t
	}

	// Reverse logs to show newest first
	reversedLogs := make([]db.LogEntry, len(logs))
	for i, l := range logs {
		reversedLogs[len(logs)-1-i] = l
	}

	state := SystemState{
		IsRunning:      isRunning,
		IsPaperTrading: isPaper,
		Balance:        balance,
		Positions:      activePositions,
		Trades:         reversedTrades,
		Logs:           reversedLogs,
		Config:         maskedCfg,
	}
	if ws.situations != nil {
		state.Situations = ws.situations()
	}
	if ws.nextTickAt != nil {
		if t := ws.nextTickAt(); !t.IsZero() {
			state.NextTickAt = t.Format(time.RFC3339)
		}
	}

	return json.Marshal(state)
}

// handleExplain returns an on-demand AI (LLM) narration of the current situation in
// Korean. POST only. When no provider/key is wired it returns a friendly 200 message
// rather than an error, so the dashboard can show guidance instead of a failure.
func (ws *WebServer) handleExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if ws.explain == nil {
		json.NewEncoder(w).Encode(map[string]string{"explanation": "AI 해설 기능이 설정되지 않았습니다."})
		return
	}
	text, err := ws.explain()
	if err != nil {
		// Surface the (Korean) guidance message as a normal response, not a 500.
		json.NewEncoder(w).Encode(map[string]string{"explanation": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"explanation": text})
}

func (ws *WebServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		data, err := ws.getSystemStateJSON()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	if r.Method == "POST" {
		var body struct {
			Action string `json:"action"` // "start", "stop", "tick", "close"
			Symbol string `json:"symbol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch body.Action {
		case "start":
			// Trigger engine start callback in main
			ws.triggerTick() // Will trigger loop start
		case "stop":
			if ws.stopBot != nil {
				ws.stopBot()
			}
		case "tick":
			go ws.triggerTick()
		case "close":
			if ws.closePos != nil && body.Symbol != "" {
				err := ws.closePos(body.Symbol)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				ws.BroadcastState()
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (ws *WebServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var newCfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Preserve sensitive keys if masked ones are sent
		current := config.GetConfig()
		if newCfg.BybitAPIKey == "******" || strings.HasPrefix(newCfg.BybitAPIKey, "******") {
			newCfg.BybitAPIKey = current.BybitAPIKey
		}
		if newCfg.BybitAPISecret == "******" {
			newCfg.BybitAPISecret = current.BybitAPISecret
		}
		if newCfg.OpenAIAPIKey == "******" {
			newCfg.OpenAIAPIKey = current.OpenAIAPIKey
		}
		if newCfg.GeminiAPIKey == "******" {
			newCfg.GeminiAPIKey = current.GeminiAPIKey
		}

		if err := config.UpdateConfig(&newCfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Update paper trading mode dynamically
		ws.setPaperMode(newCfg.IsPaperTrading)

		ws.BroadcastState()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (ws *WebServer) handleTrades(w http.ResponseWriter, r *http.Request) {
	trades, err := db.GetTrades()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trades)
}

func (ws *WebServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := db.GetLogs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

// handleBacktest runs a historical backtest for a strategy and symbol
func (ws *WebServer) handleBacktest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Symbol         string   `json:"symbol"`
		Strategy       string   `json:"strategy"`
		Interval       string   `json:"interval"`
		InitialBalance *float64 `json:"initial_balance,omitempty"`
		FeeRate        *float64 `json:"fee_rate,omitempty"`
		Candles        *int     `json:"candles,omitempty"`
		SplitRatio     float64  `json:"split_ratio,omitempty"` // 0/omitted => single full report
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if body.Symbol == "" || body.Strategy == "" || body.Interval == "" {
		http.Error(w, "필수 입력값이 누락되었습니다", http.StatusBadRequest)
		return
	}

	// The "ai" strategy calls the live LLM and reads live account balance/position on every
	// candle, so backtesting it would fire hundreds of paid API calls and leak live state into
	// a historical sim. Reject it; backtesting is for the deterministic rule strategies.
	if body.Strategy == "ai" {
		http.Error(w, "AI 전략은 백테스트할 수 없습니다(캔들마다 실시간 API를 호출함). 추세 추종, 평균 회귀, 변동성 돌파 전략을 사용하세요.", http.StatusBadRequest)
		return
	}

	// Get strategy instance
	strat, ok := ws.strategies[body.Strategy]
	if !ok {
		http.Error(w, fmt.Sprintf("'%s' 전략을 찾을 수 없습니다", body.Strategy), http.StatusBadRequest)
		return
	}

	// Resolve + validate the optional tuning params BEFORE touching the exchange,
	// so a bad request is rejected without a live fetch.
	initialBalance, feeRate, candleLimit, paramErr := resolveBacktestParams(body.InitialBalance, body.FeeRate, body.Candles)
	if paramErr != "" {
		http.Error(w, paramErr, http.StatusBadRequest)
		return
	}

	// Get exchange instance to fetch historical candles
	_, _, ex := ws.engineState()

	candles, err := ex.GetKlinesPaged(body.Symbol, body.Interval, candleLimit)
	if err != nil {
		http.Error(w, fmt.Sprintf("과거 데이터 조회 실패: %v", err), http.StatusInternalServerError)
		return
	}

	cfg := config.GetConfig()
	w.Header().Set("Content-Type", "application/json")

	// split_ratio > 0 => in-sample / out-of-sample split report; otherwise a single report.
	if body.SplitRatio > 0 {
		report, err := backtest.RunBacktestSplit(body.Symbol, candles, strat, initialBalance, cfg.RiskPercentage, cfg.Leverage, feeRate, body.SplitRatio)
		if err != nil {
			http.Error(w, fmt.Sprintf("백테스트 실행 실패: %v", err), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(report)
		return
	}

	report, err := backtest.RunBacktest(body.Symbol, candles, strat, initialBalance, cfg.RiskPercentage, cfg.Leverage, feeRate)
	if err != nil {
		http.Error(w, fmt.Sprintf("백테스트 실행 실패: %v", err), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(report)
}
