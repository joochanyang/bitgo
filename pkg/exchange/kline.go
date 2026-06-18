package exchange

import (
	"sort"
	"strconv"
	"time"
)

// klinePageLimit is Bybit's per-request cap for the /v5/market/kline endpoint.
const klinePageLimit = 1000

// mapInterval translates the bot's interval shorthand (e.g. "1h") to Bybit's
// numeric interval codes. Unknown values pass through unchanged.
func mapInterval(interval string) string {
	switch interval {
	case "1h":
		return "60"
	case "4h":
		return "240"
	case "30m":
		return "30"
	case "15m":
		return "15"
	case "5m":
		return "5"
	default:
		return interval
	}
}

// parseKlineList converts a raw Bybit kline list (newest-first rows of
// [startMs, open, high, low, close, volume, turnover]) into chronological
// (oldest-first) Candles. Rows with fewer than 6 fields are skipped.
func parseKlineList(list [][]string) []Candle {
	candles := make([]Candle, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		item := list[i]
		if len(item) < 6 {
			continue
		}
		ms, _ := strconv.ParseInt(item[0], 10, 64)
		o, _ := strconv.ParseFloat(item[1], 64)
		h, _ := strconv.ParseFloat(item[2], 64)
		l, _ := strconv.ParseFloat(item[3], 64)
		c, _ := strconv.ParseFloat(item[4], 64)
		v, _ := strconv.ParseFloat(item[5], 64)
		candles = append(candles, Candle{
			Time:   time.Unix(0, ms*int64(time.Millisecond)),
			Open:   o,
			High:   h,
			Low:    l,
			Close:  c,
			Volume: v,
		})
	}
	return candles
}

// mergeKlinePages flattens multiple candle pages into a single chronological
// slice, deduping by timestamp. When total > 0, only the most recent `total`
// candles are kept (the tail of chronological order).
func mergeKlinePages(pages [][]Candle, total int) []Candle {
	byTime := make(map[int64]Candle)
	for _, page := range pages {
		for _, c := range page {
			byTime[c.Time.UnixNano()] = c
		}
	}

	merged := make([]Candle, 0, len(byTime))
	for _, c := range byTime {
		merged = append(merged, c)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Time.Before(merged[j].Time)
	})

	if total > 0 && len(merged) > total {
		merged = merged[len(merged)-total:]
	}
	return merged
}
