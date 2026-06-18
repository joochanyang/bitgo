package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go-bot/pkg/backtest"
	"go-bot/pkg/config"
)

// maxBatchCombos caps a single batch request so it can't fan out into hundreds of
// Bybit kline fetches (rate-limit guard).
const maxBatchCombos = 20

// combo is one symbol x strategy pair to backtest.
type combo struct {
	Symbol   string
	Strategy string
}

// batchResultEntry is one combo's outcome: either Report or Error is set.
type batchResultEntry struct {
	Symbol   string                   `json:"symbol"`
	Strategy string                   `json:"strategy"`
	Report   *backtest.BacktestReport `json:"report,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

// buildCombos returns the cartesian product of symbols x strategies (symbols outer),
// deduping repeated symbols and strategies while preserving first-seen order.
func buildCombos(symbols, strategies []string) []combo {
	uSyms := dedupe(symbols)
	uStrats := dedupe(strategies)
	combos := make([]combo, 0, len(uSyms)*len(uStrats))
	for _, s := range uSyms {
		for _, st := range uStrats {
			combos = append(combos, combo{Symbol: s, Strategy: st})
		}
	}
	return combos
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// handleBacktestBatch runs a backtest for every symbol x strategy combination and
// returns one result entry per combo. A failing combo (bad symbol, "ai", unknown
// strategy) records an in-body error instead of failing the whole request.
func (ws *WebServer) handleBacktestBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Symbols    []string `json:"symbols"`
		Strategies []string `json:"strategies"`
		// Singular fallbacks so a 1x1 request (and the legacy single-combo client) works.
		Symbol         string   `json:"symbol"`
		Strategy       string   `json:"strategy"`
		Interval       string   `json:"interval"`
		InitialBalance *float64 `json:"initial_balance,omitempty"`
		FeeRate        *float64 `json:"fee_rate,omitempty"`
		Candles        *int     `json:"candles,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(body.Symbols) == 0 && body.Symbol != "" {
		body.Symbols = []string{body.Symbol}
	}
	if len(body.Strategies) == 0 && body.Strategy != "" {
		body.Strategies = []string{body.Strategy}
	}
	if body.Interval == "" || len(body.Symbols) == 0 || len(body.Strategies) == 0 {
		http.Error(w, "필수 입력값이 누락되었습니다 (심볼, 전략, 주기)", http.StatusBadRequest)
		return
	}

	initialBalance, feeRate, candleLimit, paramErr := resolveBacktestParams(body.InitialBalance, body.FeeRate, body.Candles)
	if paramErr != "" {
		http.Error(w, paramErr, http.StatusBadRequest)
		return
	}

	combos := buildCombos(body.Symbols, body.Strategies)
	if len(combos) > maxBatchCombos {
		http.Error(w, fmt.Sprintf("조합이 너무 많습니다 (%d개); 최대 %d개", len(combos), maxBatchCombos), http.StatusBadRequest)
		return
	}

	cfg := config.GetConfig()

	results := make([]batchResultEntry, 0, len(combos))
	for _, c := range combos {
		entry := batchResultEntry{Symbol: c.Symbol, Strategy: c.Strategy}
		report, errMsg := ws.runOneCombo(c, body.Interval, initialBalance, feeRate, candleLimit, cfg.RiskPercentage, cfg.Leverage)
		if errMsg != "" {
			entry.Error = errMsg
		} else {
			entry.Report = report
		}
		results = append(results, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results})
}

// runOneCombo backtests a single symbol x strategy. Returns (report, "") on success
// or (nil, errMsg) on failure — never panics, so one bad combo can't sink the batch.
func (ws *WebServer) runOneCombo(c combo, interval string, initialBalance, feeRate float64, candleLimit int, riskPct float64, leverage int) (*backtest.BacktestReport, string) {
	if c.Strategy == "ai" {
		return nil, "The AI strategy cannot be backtested (it makes live API calls per candle)."
	}
	strat, ok := ws.strategies[c.Strategy]
	if !ok {
		return nil, fmt.Sprintf("'%s' 전략을 찾을 수 없습니다", c.Strategy)
	}
	_, _, ex := ws.engineState()
	candles, err := ex.GetKlinesPaged(c.Symbol, interval, candleLimit)
	if err != nil {
		return nil, fmt.Sprintf("과거 데이터 조회 실패: %v", err)
	}
	report, err := backtest.RunBacktest(c.Symbol, candles, strat, initialBalance, riskPct, leverage, feeRate)
	if err != nil {
		return nil, fmt.Sprintf("백테스트 실행 실패: %v", err)
	}
	return report, ""
}
