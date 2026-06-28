// Command checkconn is a one-shot, read-only Bybit connection verifier.
// It loads the same .env the bot uses, builds the real BybitExchange client,
// and calls GetBalance + GetPosition. It NEVER places orders.
package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"

	"go-bot/pkg/exchange"
)

func main() {
	_ = godotenv.Load(".env")

	key := os.Getenv("BYBIT_API_KEY")
	secret := os.Getenv("BYBIT_API_SECRET")
	if key == "" || secret == "" {
		fmt.Println("❌ BYBIT_API_KEY / BYBIT_API_SECRET 가 .env에 없습니다.")
		os.Exit(1)
	}

	// isTestnet=false → mainnet, same as the bot.
	ex := exchange.NewBybitExchange(key, secret, false)

	fmt.Println("== Bybit 연결 검증 (조회 전용, 주문 없음) ==")

	bal, err := ex.GetBalance()
	if err != nil {
		fmt.Printf("❌ 잔고조회 실패: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 잔고조회 성공 — UNIFIED 계좌 자산: %.2f USDT\n", bal)

	pos, err := ex.GetPosition("WLDUSDT")
	if err != nil {
		fmt.Printf("⚠️ 포지션조회 실패: %v\n", err)
	} else if pos == nil {
		fmt.Println("✅ 포지션조회 성공 — WLDUSDT 보유 포지션 없음")
	} else {
		fmt.Printf("✅ 포지션조회 성공 — WLDUSDT %s qty=%.4f entry=%.4f\n", pos.Side, pos.Size, pos.EntryPrice)
	}

	fmt.Println("== 검증 완료: 키가 유효하고 조회 권한이 동작합니다 ==")
}
