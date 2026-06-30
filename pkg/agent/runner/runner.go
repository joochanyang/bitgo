// Package runner orchestrates one trading cycle for the AI agent: build context from
// market + memory, ask the council, validate with the guard, execute, and record the
// outcome. It wires Phase 1/2 components together without touching the live rule-bot.
package runner

import "fmt"

// classifyRegime tags the market state from where price sits in the [low, high] channel.
// Within 10% of the top -> trending_up, within 10% of the bottom -> trending_down, else
// ranging. This coarse tag is the memory recall key (matching similar past situations).
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

// episodeID builds a collision-resistant id from symbol, an opened-at timestamp (unix
// nanos), and a short nonce. The nonce/timestamp are passed in (not generated here) so
// the function stays deterministic and testable.
func episodeID(symbol string, openedAtUnixNano int64, nonce string) string {
	return fmt.Sprintf("%s-%d-%s", symbol, openedAtUnixNano, nonce)
}
