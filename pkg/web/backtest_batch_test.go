package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

func TestBuildCombos(t *testing.T) {
	t.Run("cartesian product, symbols outer", func(t *testing.T) {
		got := buildCombos([]string{"A", "B"}, []string{"s1", "s2"})
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		// symbols outer => A/s1, A/s2, B/s1, B/s2
		if got[0].Symbol != "A" || got[0].Strategy != "s1" {
			t.Errorf("got[0] = %+v, want A/s1", got[0])
		}
		if got[1].Symbol != "A" || got[1].Strategy != "s2" {
			t.Errorf("got[1] = %+v, want A/s2", got[1])
		}
	})

	t.Run("empty symbols yields no combos", func(t *testing.T) {
		if got := buildCombos(nil, []string{"s1"}); len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("duplicate symbols deduped", func(t *testing.T) {
		if got := buildCombos([]string{"A", "A"}, []string{"s1"}); len(got) != 1 {
			t.Errorf("len = %d, want 1 (deduped)", len(got))
		}
	})
}

func batchReq(t *testing.T, ws *WebServer, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/backtest/batch", strings.NewReader(body))
	ws.handleBacktestBatch(rec, req)
	return rec
}

func TestHandleBacktestBatchPartialFailure(t *testing.T) {
	ws := &WebServer{
		strategies: map[string]strategy.Strategy{
			"trend_following": holdStrategy{},
		},
		engineState: func() (bool, bool, exchange.Exchange) {
			return false, true, &fakeExchange{candles: makeFlatCandles(200), failSymbol: "BADUSDT"}
		},
	}

	body := `{"symbols":["WLDUSDT","BADUSDT"],"strategies":["trend_following","ai"],"interval":"1h"}`
	rec := batchReq(t, ws, body)
	if rec.Code != 200 {
		t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Results []struct {
			Symbol   string `json:"symbol"`
			Strategy string `json:"strategy"`
			Error    string `json:"error,omitempty"`
			Report   *struct {
				TotalTrades int `json:"total_trades"`
			} `json:"report,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 4 { // 2 symbols x 2 strategies
		t.Fatalf("results len = %d, want 4", len(resp.Results))
	}

	find := func(sym, strat string) (string, bool) {
		for _, r := range resp.Results {
			if r.Symbol == sym && r.Strategy == strat {
				if r.Report != nil {
					return "report", true
				}
				return r.Error, true
			}
		}
		return "", false
	}

	// WLDUSDT/trend_following => success
	if v, ok := find("WLDUSDT", "trend_following"); !ok || v != "report" {
		t.Errorf("WLDUSDT/trend_following = %q ok=%v, want report", v, ok)
	}
	// BADUSDT/trend_following => fetch error (not a crash)
	if v, ok := find("BADUSDT", "trend_following"); !ok || v == "report" || v == "" {
		t.Errorf("BADUSDT/trend_following = %q ok=%v, want an error", v, ok)
	}
	// any */ai => rejected per-combo
	if v, ok := find("WLDUSDT", "ai"); !ok || !strings.Contains(v, "AI strategy") {
		t.Errorf("WLDUSDT/ai = %q ok=%v, want AI rejection", v, ok)
	}
}

func TestHandleBacktestBatchLegacyFallback(t *testing.T) {
	ws := &WebServer{
		strategies: map[string]strategy.Strategy{"trend_following": holdStrategy{}},
		engineState: func() (bool, bool, exchange.Exchange) {
			return false, true, &fakeExchange{candles: makeFlatCandles(200)}
		},
	}
	// Singular fields (no arrays) must still work as a 1x1 batch.
	body := `{"symbol":"WLDUSDT","strategy":"trend_following","interval":"1h"}`
	rec := batchReq(t, ws, body)
	if rec.Code != 200 {
		t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []struct {
			Report *json.RawMessage `json:"report,omitempty"`
		} `json:"results"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 {
		t.Fatalf("results len = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].Report == nil {
		t.Errorf("expected a report for the single combo")
	}
}
