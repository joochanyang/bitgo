package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-bot/pkg/backtest"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// TestAuthorized covers the P1-4 dashboard auth gate.
func TestAuthorized(t *testing.T) {
	t.Run("no token configured allows all", func(t *testing.T) {
		ws := &WebServer{authToken: ""}
		r := httptest.NewRequest("GET", "/api/status", nil)
		if !ws.authorized(r) {
			t.Error("expected open access when no token configured")
		}
	})

	t.Run("correct header token allowed", func(t *testing.T) {
		ws := &WebServer{authToken: "secret"}
		r := httptest.NewRequest("GET", "/api/status", nil)
		r.Header.Set("X-Auth-Token", "secret")
		if !ws.authorized(r) {
			t.Error("expected access with correct X-Auth-Token")
		}
	})

	t.Run("correct query token allowed (for WS)", func(t *testing.T) {
		ws := &WebServer{authToken: "secret"}
		r := httptest.NewRequest("GET", "/ws?token=secret", nil)
		if !ws.authorized(r) {
			t.Error("expected access with correct ?token=")
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		ws := &WebServer{authToken: "secret"}
		r := httptest.NewRequest("GET", "/api/status", nil)
		r.Header.Set("X-Auth-Token", "nope")
		if ws.authorized(r) {
			t.Error("expected rejection with wrong token")
		}
	})

	t.Run("missing token rejected when configured", func(t *testing.T) {
		ws := &WebServer{authToken: "secret"}
		r := httptest.NewRequest("GET", "/api/status", nil)
		if ws.authorized(r) {
			t.Error("expected rejection when token configured but none provided")
		}
	})
}

// TestRequireAuthMiddleware verifies the wrapper returns 401 when unauthorized
// and calls through when authorized.
func TestRequireAuthMiddleware(t *testing.T) {
	ws := &WebServer{authToken: "secret"}
	called := false
	h := ws.requireAuth(func(w http.ResponseWriter, r *http.Request) { called = true })

	// Unauthorized -> 401, handler not called.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/api/status", nil))
	if rec.Code != 401 {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if called {
		t.Error("handler should not be called when unauthorized")
	}

	// Authorized -> handler runs.
	called = false
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/status", nil)
	req.Header.Set("X-Auth-Token", "secret")
	h(rec, req)
	if !called {
		t.Error("handler should be called when authorized")
	}
}

// btReq builds a JSON request body for /api/backtest.
func btReq(t *testing.T, ws *WebServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/backtest", strings.NewReader(body))
	ws.handleBacktest(rec, req)
	return rec
}

// TestHandleBacktestParamValidation covers the 400-path validation of the new
// tuning params. These never reach the exchange (validation runs first).
func TestHandleBacktestParamValidation(t *testing.T) {
	ws := &WebServer{strategies: map[string]strategy.Strategy{"trend_following": holdStrategy{}}}

	cases := []struct {
		name     string
		body     string
		wantCode int
		wantSub  string
	}{
		{"ai still rejected", `{"symbol":"X","strategy":"ai","interval":"1h"}`, 400, "백테스트할 수 없습니다"},
		{"negative balance", `{"symbol":"X","strategy":"trend_following","interval":"1h","initial_balance":-5}`, 400, "초기 자본금"},
		{"fee above cap", `{"symbol":"X","strategy":"trend_following","interval":"1h","fee_rate":0.5}`, 400, "수수료율"},
		{"negative fee", `{"symbol":"X","strategy":"trend_following","interval":"1h","fee_rate":-0.001}`, 400, "수수료율"},
		{"candles below min", `{"symbol":"X","strategy":"trend_following","interval":"1h","candles":10}`, 400, "캔들 수"},
		{"candles above max", `{"symbol":"X","strategy":"trend_following","interval":"1h","candles":99999}`, 400, "캔들 수"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := btReq(t, ws, tc.body)
			if rec.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d (body: %s)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantSub) {
				t.Errorf("body %q does not contain %q", rec.Body.String(), tc.wantSub)
			}
		})
	}
}

// TestHandleBacktestDefaultsAndOverrides covers the happy path: omitted fields use
// defaults, explicit values override, and split_ratio threads the resolved params.
func TestHandleBacktestDefaultsAndOverrides(t *testing.T) {
	newWS := func() *WebServer {
		return &WebServer{
			strategies: map[string]strategy.Strategy{"trend_following": holdStrategy{}},
			engineState: func() (bool, bool, exchange.Exchange) {
				return false, true, &fakeExchange{candles: makeFlatCandles(200)}
			},
		}
	}

	t.Run("omitted fields use default balance 10000", func(t *testing.T) {
		rec := btReq(t, newWS(), `{"symbol":"BTCUSDT","strategy":"trend_following","interval":"1h"}`)
		if rec.Code != 200 {
			t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var rep backtest.BacktestReport
		if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rep.InitialBalance != 10000.0 {
			t.Errorf("InitialBalance = %v, want 10000 (default)", rep.InitialBalance)
		}
	})

	t.Run("explicit balance overrides", func(t *testing.T) {
		rec := btReq(t, newWS(), `{"symbol":"BTCUSDT","strategy":"trend_following","interval":"1h","initial_balance":25000}`)
		if rec.Code != 200 {
			t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var rep backtest.BacktestReport
		json.Unmarshal(rec.Body.Bytes(), &rep)
		if rep.InitialBalance != 25000.0 {
			t.Errorf("InitialBalance = %v, want 25000", rep.InitialBalance)
		}
	})

	t.Run("zero balance and zero candles fall back to defaults", func(t *testing.T) {
		rec := btReq(t, newWS(), `{"symbol":"BTCUSDT","strategy":"trend_following","interval":"1h","initial_balance":0,"candles":0}`)
		if rec.Code != 200 {
			t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var rep backtest.BacktestReport
		json.Unmarshal(rec.Body.Bytes(), &rep)
		if rep.InitialBalance != 10000.0 {
			t.Errorf("InitialBalance = %v, want 10000 (0 treated as omitted)", rep.InitialBalance)
		}
	})

	t.Run("split_ratio threads resolved balance into both segments (G2)", func(t *testing.T) {
		rec := btReq(t, newWS(), `{"symbol":"BTCUSDT","strategy":"trend_following","interval":"1h","initial_balance":25000,"split_ratio":0.5}`)
		if rec.Code != 200 {
			t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var sr backtest.SplitReport
		if err := json.Unmarshal(rec.Body.Bytes(), &sr); err != nil {
			t.Fatalf("decode SplitReport: %v", err)
		}
		if sr.InSample == nil || sr.OutOfSample == nil {
			t.Fatalf("expected split report with both segments, got %+v", sr)
		}
		if sr.InSample.InitialBalance != 25000.0 {
			t.Errorf("in-sample InitialBalance = %v, want 25000 (resolved param must thread into split)", sr.InSample.InitialBalance)
		}
	})
}
