package bot

import "fmt"

// MarketView is a snapshot of what the bot sees for one symbol at the latest tick.
// The engine records it after each evaluation so the dashboard can explain, in plain
// Korean, what the bot is doing and why — without the frontend re-deriving strategy math.
type MarketView struct {
	Symbol        string  `json:"symbol"`
	IsRunning     bool    `json:"is_running"`    // engine running at snapshot time
	HasPosition   bool    `json:"has_position"`  // an open position exists
	Side          string  `json:"side"`          // "LONG" / "SHORT" when HasPosition
	Decision      string  `json:"decision"`      // last strategy decision: LONG/SHORT/HOLD/CLOSE
	CurrentPrice  float64 `json:"current_price"` // latest close
	EntryPrice    float64 `json:"entry_price"`   // position entry (when HasPosition)
	StopLossPrice float64 `json:"stop_loss_price"`
	TakeProfitPct float64 `json:"take_profit_price"` // exit target price (json kept for symmetry with SL)
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Reasoning     string  `json:"reasoning"` // strategy's own (English) reasoning, surfaced as-is
}

// Situation is the beginner-friendly, two-line Korean explanation of a MarketView.
type Situation struct {
	Headline string `json:"headline"` // one short line: the state
	Detail   string `json:"detail"`   // a sentence or two explaining it in plain terms
}

// SymbolStatus bundles the raw snapshot with its plain-Korean explanation for the
// dashboard. The frontend shows Situation; View carries the numbers (entry/SL/TP) the
// chart price-lines need.
type SymbolStatus struct {
	View      MarketView `json:"view"`
	Situation Situation  `json:"situation"`
}

// describeSituation turns a MarketView into plain Korean a first-timer can follow.
// Pure function: no I/O, fully unit-tested.
func describeSituation(mv MarketView) Situation {
	// Engine stopped: nothing is happening until the user presses start.
	if !mv.IsRunning && !mv.HasPosition {
		return Situation{
			Headline: "⏸ 봇 정지됨 (꺼짐)",
			Detail:   fmt.Sprintf("봇이 멈춰 있어요. '봇 시작'을 누르면 %s 시세를 분석해 매매를 시작합니다.", symbolOr(mv.Symbol)),
		}
	}

	// Holding a position: explain side, entry, and profit/loss state.
	if mv.HasPosition {
		sideKo := "롱(상승 베팅)"
		if mv.Side == "SHORT" {
			sideKo = "숏(하락 베팅)"
		}
		headline := fmt.Sprintf("📌 %s 보유 중 — %s", mv.Symbol, sideKo)

		pnlWord := "본전 부근"
		if mv.UnrealizedPnL > 0 {
			pnlWord = fmt.Sprintf("현재 수익 +%.2f USDT", mv.UnrealizedPnL)
		} else if mv.UnrealizedPnL < 0 {
			pnlWord = fmt.Sprintf("현재 손실 %.2f USDT", mv.UnrealizedPnL)
		}

		detail := fmt.Sprintf("%.4f 가격에 진입했고 지금은 %.4f예요. %s.",
			mv.EntryPrice, mv.CurrentPrice, pnlWord)
		if mv.StopLossPrice > 0 {
			detail += fmt.Sprintf(" 손절은 %.4f, 익절 목표는 %.4f입니다.", mv.StopLossPrice, mv.TakeProfitPct)
		}
		return Situation{Headline: headline, Detail: detail}
	}

	// No position, engine running: waiting for an entry signal.
	headline := "🔍 진입 신호 대기 중"
	detail := fmt.Sprintf("%s 가격은 현재 %.4f이고, 봇이 진입 조건이 맞는지 지켜보는 중입니다. 조건이 맞으면 자동으로 진입해요.",
		mv.Symbol, mv.CurrentPrice)
	return Situation{Headline: headline, Detail: detail}
}

// symbolOr returns the symbol or a friendly fallback when it's unset.
func symbolOr(sym string) string {
	if sym == "" {
		return "설정한 코인"
	}
	return sym
}
