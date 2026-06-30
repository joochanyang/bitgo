// Command agent is the standalone AI trading agent: it runs the council -> guard ->
// execute -> memory cycle on a timer, in paper mode (no real orders). It is fully
// separate from the live rule-bot (pkg/bot) and its web dashboard.
package main

import "time"

// classifyRegime tags the market state from where price sits in the [low, high] channel.
// Within 10% of the top -> trending_up, within 10% of the bottom -> trending_down, else
// ranging. This MUST stay identical to runner.classifyRegime so memory recall keys match.
func classifyRegime(price, low, high float64) string {
	if high <= low {
		return "ranging"
	}
	span := high - low
	if price >= high-0.1*span {
		return "trending_up"
	}
	if price <= low+0.1*span {
		return "trending_down"
	}
	return "ranging"
}

// tickInterval maps a config interval string to the loop period. Unknown values fall
// back to 4h (the validated default timeframe for the breakout edge).
func tickInterval(interval string) time.Duration {
	switch interval {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		return 4 * time.Hour
	}
}
