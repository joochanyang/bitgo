// Package notify sends best-effort trade notifications to Telegram.
//
// The notifier is nil-safe: when the bot token or chat id is missing, New
// returns nil and every method on a nil *Notifier is a no-op. This lets the
// engine call e.notifier.Send(...) unconditionally without guarding for the
// "no Telegram configured" case (e.g. paper trading, or env not set).
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-bot/pkg/db"
)

// Notifier posts messages to a Telegram chat via the Bot API.
type Notifier struct {
	botToken string
	chatID   string
	client   *http.Client
	apiBase  string // overridable in tests; defaults to https://api.telegram.org
}

// New returns a Notifier, or nil if either credential is empty. A nil Notifier
// is a valid no-op receiver, so callers never need to nil-check before Send.
func New(botToken, chatID string) *Notifier {
	if botToken == "" || chatID == "" {
		return nil
	}
	return &Notifier{
		botToken: botToken,
		chatID:   chatID,
		client:   &http.Client{Timeout: 10 * time.Second},
		apiBase:  "https://api.telegram.org",
	}
}

// WithAPIBase overrides the Telegram API base URL (default
// https://api.telegram.org). Useful for routing through a proxy, or for tests
// that capture sends against a local server. Returns the receiver for chaining;
// a nil receiver is returned unchanged.
func (n *Notifier) WithAPIBase(base string) *Notifier {
	if n != nil && base != "" {
		n.apiBase = base
	}
	return n
}

// Send posts text to the configured chat. It is best-effort: a nil receiver or
// any network/API error is swallowed (logged as a warning) so notification
// failure never interrupts trading. Messages use HTML parse mode so <b> bold
// renders in the Telegram client.
func (n *Notifier) Send(text string) {
	if n == nil {
		return
	}
	payload := map[string]string{
		"chat_id":    n.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		db.LogWarn("Telegram notify: marshal failed: %v", err)
		return
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", n.apiBase, n.botToken)
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		db.LogWarn("Telegram notify: send failed: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		db.LogWarn("Telegram notify: API returned status %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Message builders. Each returns a self-contained, plain-Korean, emoji-led
// message so a glance tells the user exactly what happened to their money.
// ---------------------------------------------------------------------------

// modeTag renders the trading mode as a clear, bracketed tag.
func modeTag(paper bool) string {
	if paper {
		return "🧪 모의(페이퍼)"
	}
	return "💵 실거래"
}

// sideKR renders LONG/SHORT in Korean with a directional emoji.
func sideKR(side string) string {
	switch strings.ToUpper(side) {
	case "LONG":
		return "🔺 롱(상승 베팅)"
	case "SHORT":
		return "🔻 숏(하락 베팅)"
	default:
		return side
	}
}

// signed formats a number with an explicit +/- sign and fixed decimals.
func signed(v float64, dec int) string {
	return fmt.Sprintf("%+.*f", dec, v)
}

// FormatStart announces the engine starting, with the full active configuration
// so the user knows exactly what the bot will do.
func FormatStart(strategyName, interval string, symbols []string, leverage int, riskPct, balance float64, paper bool) string {
	return fmt.Sprintf(
		"🚀 <b>봇 시작</b>  (%s)\n"+
			"━━━━━━━━━━━━━━\n"+
			"전략: <b>%s</b>\n"+
			"봉 주기: %s   레버리지: %dx\n"+
			"감시 종목: %s\n"+
			"건당 리스크: %.1f%%   현재 잔고: %.2f USDT\n"+
			"━━━━━━━━━━━━━━\n"+
			"이제부터 %s 봉마다 분석해서 신호가 나오면 자동 매매합니다.",
		modeTag(paper), strategyName, interval, leverage,
		strings.Join(symbols, ", "), riskPct, balance, interval,
	)
}

// FormatStop announces the engine stopping.
func FormatStop(paper bool) string {
	return fmt.Sprintf("🛑 <b>봇 정지</b>  (%s)\n자동 매매를 멈췄습니다. 열린 포지션의 거래소측 손절은 그대로 유지됩니다.", modeTag(paper))
}

// FormatOpen announces a newly opened position with the full rationale.
func FormatOpen(symbol, side string, qty, entry, sl, tp float64, leverage int, confidence, riskUSDT, notionalUSDT float64, reason string, paper bool) string {
	slPct := pctDiff(entry, sl)
	tpPct := pctDiff(entry, tp)
	msg := fmt.Sprintf(
		"🟢 <b>신규 진입</b>  (%s)\n"+
			"━━━━━━━━━━━━━━\n"+
			"종목: <b>%s</b>\n"+
			"방향: %s\n"+
			"진입가: <b>%.4f</b>\n"+
			"수량: %.4f   레버리지: %dx\n"+
			"명목 규모: %.2f USDT\n"+
			"━━━━━━━━━━━━━━\n"+
			"🛑 손절: %.4f  (%.2f%%, 최대손실 ≈ %.2f USDT)\n"+
			"🎯 익절: %.4f  (%.2f%%)\n",
		modeTag(paper), symbol, sideKR(side), entry, qty, leverage,
		notionalUSDT, sl, slPct, riskUSDT, tp, tpPct,
	)
	if confidence > 0 {
		msg += fmt.Sprintf("신뢰도: %.0f%%\n", confidence*100)
	}
	if strings.TrimSpace(reason) != "" {
		msg += fmt.Sprintf("📌 근거: %s", reason)
	}
	return strings.TrimRight(msg, "\n")
}

// FormatClose announces a position closed by a strategy CLOSE decision.
func FormatClose(symbol, side string, qty, entry, exit, pnl float64, heldFor time.Duration, paper bool) string {
	return formatExit("🔚 <b>포지션 청산</b>", "전략 청산 신호", symbol, side, qty, entry, exit, pnl, heldFor, paper)
}

// FormatFlip announces closing an opposite position right before reversing.
func FormatFlip(symbol, side string, qty, entry, exit, pnl float64, heldFor time.Duration, paper bool) string {
	return formatExit("🔄 <b>방향 전환 (기존 청산)</b>", "반대 신호로 전환", symbol, side, qty, entry, exit, pnl, heldFor, paper)
}

// FormatStopHit announces a position the exchange closed on its own — a hard
// stop-loss or take-profit fill detected on sync. This is the most important
// alert: it means the bot's protective order actually fired.
func FormatStopHit(symbol, side string, qty, entry, exit, pnl float64, paper bool) string {
	title := "🎯 <b>익절 체결 (자동)</b>"
	cause := "익절가 도달 — 거래소가 자동 청산"
	if pnl < 0 {
		title = "🛑 <b>손절 체결 (자동)</b>"
		cause = "손절가 도달 — 거래소가 자동 청산"
	}
	return formatExit(title, cause, symbol, side, qty, entry, exit, pnl, 0, paper)
}

// formatExit is the shared body for all position-closing notifications.
func formatExit(title, cause, symbol, side string, qty, entry, exit, pnl float64, heldFor time.Duration, paper bool) string {
	resultEmoji := "⚪"
	resultWord := "본전"
	if pnl > 0 {
		resultEmoji, resultWord = "✅", "수익"
	} else if pnl < 0 {
		resultEmoji, resultWord = "❌", "손실"
	}
	roiPct := pnlPct(side, entry, exit)
	msg := fmt.Sprintf(
		"%s  (%s)\n"+
			"━━━━━━━━━━━━━━\n"+
			"종목: <b>%s</b>   방향: %s\n"+
			"진입 %.4f → 청산 <b>%.4f</b>\n"+
			"수량: %.4f\n"+
			"━━━━━━━━━━━━━━\n"+
			"%s 결과: <b>%s %s USDT</b> (%s%%)\n"+
			"사유: %s",
		title, modeTag(paper), symbol, sideKR(side), entry, exit, qty,
		resultEmoji, resultWord, signed(pnl, 2), signed(roiPct, 2), cause,
	)
	if heldFor > 0 {
		msg += fmt.Sprintf("\n보유 시간: %s", humanDuration(heldFor))
	}
	return msg
}

// FormatTrailing announces a trailing-stop tighten (locking in more profit).
func FormatTrailing(symbol, side string, oldSL, newSL, price float64, paper bool) string {
	return fmt.Sprintf(
		"🔧 <b>손절선 이동</b>  (%s)\n"+
			"%s %s — 손절가를 %.4f → <b>%.4f</b> 로 올렸습니다.\n"+
			"현재가 %.4f 기준으로 수익을 더 보호합니다.",
		modeTag(paper), symbol, sideKR(side), oldSL, newSL, price,
	)
}

// FormatSkip announces an entry skipped because the portfolio risk budget is full.
func FormatSkip(symbol string, maxPortfolioRiskPct float64, paper bool) string {
	return fmt.Sprintf(
		"⏭️ <b>진입 보류</b>  (%s)\n"+
			"%s 진입 신호가 있었지만, 이미 열린 포지션들의 합산 리스크가 한도(%.1f%%)에 도달해 이번엔 들어가지 않았습니다.",
		modeTag(paper), symbol, maxPortfolioRiskPct,
	)
}

// FormatError announces a trading error for a symbol.
func FormatError(symbol string, err error) string {
	return fmt.Sprintf("⚠️ <b>오류</b> (%s)\n%v", symbol, err)
}

// ---------------------------------------------------------------------------
// Pure numeric/format helpers.
// ---------------------------------------------------------------------------

// pctDiff returns the absolute percent distance of b from a (a as base).
func pctDiff(a, b float64) float64 {
	if a == 0 {
		return 0
	}
	d := (b - a) / a * 100
	if d < 0 {
		d = -d
	}
	return d
}

// pnlPct returns the position's ROI percent based on side and entry/exit price
// (price move only, leverage-agnostic — matches how UnrealizedPnL is reported).
func pnlPct(side string, entry, exit float64) float64 {
	if entry == 0 {
		return 0
	}
	if strings.ToUpper(side) == "SHORT" {
		return (entry - exit) / entry * 100
	}
	return (exit - entry) / entry * 100
}

// humanDuration renders a duration as a compact Korean string (e.g. "3시간 12분").
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d초", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%d분", m)
	}
	if m == 0 {
		return fmt.Sprintf("%d시간", h)
	}
	return fmt.Sprintf("%d시간 %d분", h, m)
}
