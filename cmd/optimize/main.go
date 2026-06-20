// Command optimize runs a grid parameter sweep for the volatility_breakout strategy
// against real Bybit historical candles, scoring each parameter combo on an
// out-of-sample (walk-forward) split so overfit combos are filtered out.
//
// It does NOT touch live trading or config — it only reads public klines and prints
// a ranked report of OOS-robust parameter sets. Pick a winner, then set it as the
// new default in volatility_breakout.go (a separate, deliberate code change).
//
// Usage:
//
//	go run ./cmd/optimize -symbols WLDUSDT,NEARUSDT,RENDERUSDT -interval 4h
//	go run ./cmd/optimize -symbols WLDUSDT -lookbacks 10,20,30 -rr 1.5,2,2.5 -atrk 1,1.5,2
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"go-bot/pkg/backtest"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

const (
	// Match the web backtester defaults so sweep numbers are comparable.
	initialBalance = 10000.0
	feeRate        = 0.0006
	splitRatio     = 0.7 // 70% in-sample / 30% out-of-sample
)

// comboResult holds one parameter set and its in/out-of-sample performance,
// aggregated across all symbols.
type comboResult struct {
	lookback   int
	rewardRisk float64
	atrK       float64

	// Per-symbol OOS reports kept for the detail table.
	perSymbol []symbolOutcome

	// Aggregates (means across symbols that produced a valid OOS report).
	avgOOSReturn float64
	avgOOSPF     float64
	avgOOSMDD    float64
	oosPassCount int // symbols whose OOS return > 0
	symbolCount  int
}

type symbolOutcome struct {
	symbol    string
	inReturn  float64
	oosReturn float64
	oosPF     float64
	oosMDD    float64
	oosTrades int
	err       string
}

func main() {
	symbolsFlag := flag.String("symbols", "WLDUSDT,NEARUSDT,RENDERUSDT", "comma-separated symbols to sweep")
	interval := flag.String("interval", "4h", "candle interval (5m,15m,30m,1h,4h)")
	candles := flag.Int("candles", 1000, "candles to fetch per symbol (max 1000)")
	lookbacksFlag := flag.String("lookbacks", "10,15,20,30,40", "comma-separated breakout lookbacks")
	rrFlag := flag.String("rr", "1.5,2.0,2.5,3.0", "comma-separated reward:risk ratios")
	atrkFlag := flag.String("atrk", "1.0,1.5,2.0", "comma-separated ATR stop multipliers")
	riskPct := flag.Float64("risk", 1.0, "risk percent per trade (matches live sizing)")
	leverage := flag.Int("leverage", 3, "leverage (margin only; notional cap)")
	topN := flag.Int("top", 10, "how many top combos to print")
	flag.Parse()

	symbols := splitNonEmpty(*symbolsFlag)
	lookbacks := parseInts(*lookbacksFlag)
	rrs := parseFloats(*rrFlag)
	atrks := parseFloats(*atrkFlag)
	if len(symbols) == 0 || len(lookbacks) == 0 || len(rrs) == 0 || len(atrks) == 0 {
		fmt.Fprintln(os.Stderr, "error: symbols/lookbacks/rr/atrk must each have at least one value")
		os.Exit(1)
	}

	// Public client — klines need no API key (mainnet public data).
	ex := exchange.NewBybitExchange("", "", false)

	// Fetch candles once per symbol (the heavy network cost), reuse across all combos.
	candlesBySymbol := make(map[string][]exchange.Candle, len(symbols))
	for _, sym := range symbols {
		cs, err := ex.GetKlinesPaged(sym, *interval, *candles)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %s: kline fetch failed: %v (skipping)\n", sym, err)
			continue
		}
		candlesBySymbol[sym] = cs
		fmt.Fprintf(os.Stderr, "fetched %d %s candles for %s\n", len(cs), *interval, sym)
	}
	if len(candlesBySymbol) == 0 {
		fmt.Fprintln(os.Stderr, "error: no candle data fetched for any symbol")
		os.Exit(1)
	}

	totalCombos := len(lookbacks) * len(rrs) * len(atrks)
	fmt.Fprintf(os.Stderr, "sweeping %d combos x %d symbols (risk=%.2f%% lev=%d split=%.0f/%.0f)\n\n",
		totalCombos, len(candlesBySymbol), *riskPct, *leverage, splitRatio*100, (1-splitRatio)*100)

	var results []comboResult
	for _, lb := range lookbacks {
		for _, rr := range rrs {
			for _, k := range atrks {
				res := comboResult{lookback: lb, rewardRisk: rr, atrK: k}
				strat := strategy.NewVolatilityBreakoutWithParams(lb, rr, k)

				var sumRet, sumPF, sumMDD float64
				for _, sym := range symbols {
					cs, ok := candlesBySymbol[sym]
					if !ok {
						continue
					}
					out := symbolOutcome{symbol: sym}
					split, err := backtest.RunBacktestSplit(sym, cs, strat, initialBalance, *riskPct, *leverage, feeRate, splitRatio)
					if err != nil {
						out.err = err.Error()
						res.perSymbol = append(res.perSymbol, out)
						continue
					}
					out.inReturn = split.InSample.TotalReturnPct
					out.oosReturn = split.OutOfSample.TotalReturnPct
					out.oosPF = split.OutOfSample.ProfitFactor
					out.oosMDD = split.OutOfSample.MaxDrawdownPct
					out.oosTrades = split.OutOfSample.TotalTrades
					res.perSymbol = append(res.perSymbol, out)

					sumRet += out.oosReturn
					sumPF += out.oosPF
					sumMDD += out.oosMDD
					res.symbolCount++
					if out.oosReturn > 0 {
						res.oosPassCount++
					}
				}
				if res.symbolCount > 0 {
					res.avgOOSReturn = sumRet / float64(res.symbolCount)
					res.avgOOSPF = sumPF / float64(res.symbolCount)
					res.avgOOSMDD = sumMDD / float64(res.symbolCount)
				}
				results = append(results, res)
			}
		}
	}

	rankAndPrint(results, symbols, *topN)
}

// rankAndPrint sorts combos by robustness and prints a ranked summary plus a
// per-symbol OOS detail for the top combos.
func rankAndPrint(results []comboResult, symbols []string, topN int) {
	// Rank: more symbols passing OOS first, then higher avg OOS return.
	// A combo that only wins on one symbol is less trustworthy than one that
	// generalizes across all of them.
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].oosPassCount != results[j].oosPassCount {
			return results[i].oosPassCount > results[j].oosPassCount
		}
		return results[i].avgOOSReturn > results[j].avgOOSReturn
	})

	fmt.Printf("=== Parameter sweep results (ranked by OOS robustness) ===\n")
	fmt.Printf("%-4s %-9s %-5s %-6s | %-8s %-9s %-8s %-8s\n",
		"lb", "reward:r", "atrK", "OOSok", "avgOOS%", "avgPF", "avgMDD%", "symbols")
	fmt.Println(strings.Repeat("-", 72))

	limit := topN
	if limit > len(results) {
		limit = len(results)
	}
	for i := 0; i < limit; i++ {
		r := results[i]
		fmt.Printf("%-4d %-9.2f %-5.2f %d/%d   | %+8.2f %-9.2f %-8.2f %d\n",
			r.lookback, r.rewardRisk, r.atrK, r.oosPassCount, r.symbolCount,
			r.avgOOSReturn, r.avgOOSPF, r.avgOOSMDD, r.symbolCount)
	}

	fmt.Printf("\n=== Per-symbol OOS detail for top %d ===\n", limit)
	for i := 0; i < limit; i++ {
		r := results[i]
		fmt.Printf("\n[#%d] lookback=%d reward:risk=%.2f atrK=%.2f\n", i+1, r.lookback, r.rewardRisk, r.atrK)
		fmt.Printf("  %-12s %-9s %-9s %-7s %-8s %-6s\n", "symbol", "in%", "oos%", "oosPF", "oosMDD%", "trades")
		for _, s := range r.perSymbol {
			if s.err != "" {
				fmt.Printf("  %-12s  error: %s\n", s.symbol, s.err)
				continue
			}
			fmt.Printf("  %-12s %+9.2f %+9.2f %-7.2f %-8.2f %d\n",
				s.symbol, s.inReturn, s.oosReturn, s.oosPF, s.oosMDD, s.oosTrades)
		}
	}

	fmt.Printf("\nNote: rank prefers combos that stay positive OOS across MORE symbols\n")
	fmt.Printf("(generalization) over a single-symbol spike. Pick a winner, then set it\n")
	fmt.Printf("as the default in pkg/strategy/volatility_breakout.go as a separate change.\n")
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func parseInts(s string) []int {
	var out []int
	for _, p := range splitNonEmpty(s) {
		v, err := strconv.Atoi(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping non-integer %q\n", p)
			continue
		}
		out = append(out, v)
	}
	return out
}

func parseFloats(s string) []float64 {
	var out []float64
	for _, p := range splitNonEmpty(s) {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping non-number %q\n", p)
			continue
		}
		out = append(out, v)
	}
	return out
}
