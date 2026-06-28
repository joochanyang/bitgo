// Command checktg sends one test message through the bot's real notify package,
// verifying the Telegram token + chat id end-to-end. One-shot, sends only.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"go-bot/pkg/notify"
)

func main() {
	_ = godotenv.Load(".env")
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chat := os.Getenv("TELEGRAM_CHAT_ID")

	n := notify.New(token, chat)
	if n == nil {
		fmt.Println("❌ 텔레그램 비활성: TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID 중 하나가 비었습니다.")
		os.Exit(1)
	}

	// Send a sample of every message type the bot will actually send, so the
	// user can see exactly what each alert looks like.
	paper := true
	n.Send("🤖 ANTIGRAVITY 봇 텔레그램 알림 연결 테스트 — 아래는 실제로 받게 될 알림 예시입니다.")
	n.Send(notify.FormatStart("volatility_breakout", "4h", []string{"WLDUSDT"}, 3, 1.0, 50.0, paper))
	n.Send(notify.FormatOpen("WLDUSDT", "LONG", 30.0, 0.6065, 0.5913, 0.6309, 3, 0.72, 0.46, 18.2, "20봉 채널 상단 돌파 + 거래량 확인", paper))
	n.Send(notify.FormatTrailing("WLDUSDT", "LONG", 0.5913, 0.6010, 0.6200, paper))
	n.Send(notify.FormatStopHit("WLDUSDT", "LONG", 30.0, 0.6065, 0.6309, 7.32, paper))
	n.Send(notify.FormatClose("WLDUSDT", "LONG", 30.0, 0.6065, 0.6309, 12.34, 3*time.Hour+12*time.Minute, paper))
	n.Send(notify.FormatFlip("WLDUSDT", "SHORT", 30.0, 0.6300, 0.6065, 7.05, time.Hour+30*time.Minute, paper))
	n.Send(notify.FormatSkip("NEARUSDT", 10.0, paper))
	n.Send(notify.FormatStop(paper))

	fmt.Println("✅ 테스트 메시지 9건 발송 시도 완료. 텔레그램에서 수신 확인하세요.")
	fmt.Println("   (오류가 있었다면 위에 [WARN] 로그가 찍힙니다.)")
}
