package bot

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-bot/pkg/config"
	"go-bot/pkg/db"
	"go-bot/pkg/exchange"
	"go-bot/pkg/notify"
	"go-bot/pkg/strategy"
)

// fakeExchange is a no-network Exchange for testing the engine's notifier wiring.
// It records whether an order was placed and returns canned position/balance data.
type fakeExchange struct {
	pos       *exchange.Position
	placed    bool
	closed    bool
	fillPrice float64
}

func (f *fakeExchange) GetTicker(string) (float64, error) { return f.fillPrice, nil }
func (f *fakeExchange) GetKlines(string, string, int) ([]exchange.Candle, error) {
	return nil, nil
}
func (f *fakeExchange) GetKlinesPaged(string, string, int) ([]exchange.Candle, error) {
	return nil, nil
}
func (f *fakeExchange) GetBalance() (float64, error) { return 1000, nil }
func (f *fakeExchange) GetPosition(string) (*exchange.Position, error) {
	if f.pos != nil {
		return f.pos, nil
	}
	return &exchange.Position{Side: "NONE", Size: 0}, nil
}
func (f *fakeExchange) PlaceOrder(symbol, side string, qty, price float64, opts exchange.OrderOptions) (*exchange.OrderResult, error) {
	f.placed = true
	return &exchange.OrderResult{OrderID: "test-order", Price: f.fillPrice}, nil
}
func (f *fakeExchange) ClosePosition(string) error        { f.closed = true; return nil }
func (f *fakeExchange) SetLeverage(string, int) error     { return nil }
func (f *fakeExchange) SetStopLoss(string, float64) error { return nil }

// capturer records the bodies of inbound Telegram sendMessage requests.
type capturer struct {
	mu     sync.Mutex
	bodies []string
}

func (c *capturer) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, string(b))
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
}

func (c *capturer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func (c *capturer) waitFor(n int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c.count() >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return c.count() >= n
}

func (c *capturer) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.bodies, "\n---\n")
}

func newTestEngine(ex exchange.Exchange) *Engine {
	return &Engine{
		Exchange:    ex,
		Config:      config.GetConfig(),
		stopChan:    make(chan struct{}),
		Strategies:  map[string]strategy.Strategy{},
		marketViews: make(map[string]MarketView),
	}
}

func TestNotifierFiresOnOpen(t *testing.T) {
	cap := &capturer{}
	srv := cap.server()
	defer srv.Close()

	n := notify.New("TOKEN", "CHAT").WithAPIBase(srv.URL)

	fx := &fakeExchange{fillPrice: 0.6}
	e := newTestEngine(fx)
	e.SetNotifier(n)

	cfg := &config.Config{
		IsPaperTrading:      false,
		Leverage:            3,
		RiskPercentage:      1.0,
		MaxPortfolioRiskPct: 10.0,
	}
	decision := &strategy.Decision{
		Decision:      strategy.LONG,
		StopLossPct:   2.0,
		TakeProfitPct: 4.0,
		Leverage:      3,
		Confidence:    0.7,
		Reasoning:     "채널 돌파",
	}
	pos := &exchange.Position{Side: "NONE", Size: 0}

	if err := e.executeDecision(fx, cfg, "WLDUSDT", decision, pos, 1000, 0.6); err != nil {
		t.Fatalf("executeDecision returned error: %v", err)
	}
	if !fx.placed {
		t.Fatal("expected an order to be placed")
	}
	if !cap.waitFor(1, 2*time.Second) {
		t.Fatalf("expected >=1 Telegram send on open, got %d", cap.count())
	}
	if body := cap.joined(); !strings.Contains(body, "신규 진입") || !strings.Contains(body, "채널 돌파") {
		t.Errorf("open message missing expected content:\n%s", body)
	}
}

// TestNotifierFiresOnStopHit proves the most important alert: when the exchange
// closes a position on its own (hard SL/TP fill), syncTradeHistory detects it
// and sends a stop-hit notification.
func TestNotifierFiresOnStopHit(t *testing.T) {
	cap := &capturer{}
	srv := cap.server()
	defer srv.Close()
	n := notify.New("TOKEN", "CHAT").WithAPIBase(srv.URL)

	// Seed an OPEN trade in the db, then make the exchange report no position
	// (i.e. it closed on its own).
	db.AddTrade(db.Trade{
		ID: "stophit-1", Symbol: "STOPUSDT", Side: "LONG", Size: 10,
		EntryPrice: 1.0, Leverage: 3, Timestamp: time.Now().Add(-time.Hour),
		IsPaper: false, Status: "OPEN",
	})
	defer func() {
		// best-effort cleanup: mark closed so it doesn't leak into other tests
		_ = db.UpdateTrade(db.Trade{ID: "stophit-1", Symbol: "STOPUSDT", Side: "LONG", Status: "CLOSED"})
	}()

	fx := &fakeExchange{
		pos:       &exchange.Position{Symbol: "STOPUSDT", Side: "NONE", Size: 0}, // closed on exchange
		fillPrice: 1.1,                                                           // exit price → +profit (take-profit hit)
	}
	e := newTestEngine(fx)
	e.SetNotifier(n)

	e.syncTradeHistory()

	if !cap.waitFor(1, 2*time.Second) {
		t.Fatalf("expected a stop-hit Telegram send, got %d", cap.count())
	}
	if body := cap.joined(); !strings.Contains(body, "체결") || !strings.Contains(body, "STOPUSDT") {
		t.Errorf("stop-hit message missing expected content:\n%s", body)
	}
}

func TestNilNotifierDoesNotPanicOnOpen(t *testing.T) {
	fx := &fakeExchange{fillPrice: 0.6}
	e := newTestEngine(fx)
	// No SetNotifier → e.notifier is nil. Must not panic.
	cfg := &config.Config{Leverage: 3, RiskPercentage: 1.0, MaxPortfolioRiskPct: 10.0}
	decision := &strategy.Decision{Decision: strategy.LONG, StopLossPct: 2.0, TakeProfitPct: 4.0, Leverage: 3}
	pos := &exchange.Position{Side: "NONE", Size: 0}
	if err := e.executeDecision(fx, cfg, "WLDUSDT", decision, pos, 1000, 0.6); err != nil {
		t.Fatalf("executeDecision returned error: %v", err)
	}
}
