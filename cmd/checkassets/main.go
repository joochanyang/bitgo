// Command checkassets is a one-shot, READ-ONLY asset locator.
// It signs requests exactly like the bot (HMAC-SHA256, GET rule) and queries
// where the user's funds live across Bybit account types. NO orders, NO transfers.
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

const (
	apiURL     = "https://api.bybit.com"
	recvWindow = "5000"
)

var (
	apiKey    string
	apiSecret string
	client    = &http.Client{Timeout: 10 * time.Second}
)

func sign(ts, qs string) string {
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(ts + apiKey + recvWindow + qs))
	return hex.EncodeToString(h.Sum(nil))
}

func get(path string, params url.Values) (json.RawMessage, error) {
	qs := params.Encode()
	full := apiURL + path
	if qs != "" {
		full += "?" + qs
	}
	req, _ := http.NewRequest("GET", full, nil)
	ts := fmt.Sprintf("%d", time.Now().UnixNano()/int64(time.Millisecond))
	req.Header.Set("X-BAPI-API-KEY", apiKey)
	req.Header.Set("X-BAPI-TIMESTAMP", ts)
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)
	req.Header.Set("X-BAPI-SIGN", sign(ts, qs))
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		RetCode int             `json:"retCode"`
		RetMsg  string          `json:"retMsg"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("parse: %v body=%s", err, string(body))
	}
	if env.RetCode != 0 {
		return nil, fmt.Errorf("retCode %d: %s", env.RetCode, env.RetMsg)
	}
	return env.Result, nil
}

// printCoins prints non-zero coin balances from a wallet-balance result.
func walletBalance(accountType string) {
	res, err := get("/v5/account/wallet-balance", url.Values{"accountType": {accountType}})
	if err != nil {
		fmt.Printf("  [%s] 조회 실패: %v\n", accountType, err)
		return
	}
	var r struct {
		List []struct {
			TotalEquity string `json:"totalEquity"`
			Coin        []struct {
				Coin          string `json:"coin"`
				Equity        string `json:"equity"`
				WalletBalance string `json:"walletBalance"`
				USDValue      string `json:"usdValue"`
			} `json:"coin"`
		} `json:"list"`
	}
	json.Unmarshal(res, &r)
	if len(r.List) == 0 {
		fmt.Printf("  [%s] 잔고 없음\n", accountType)
		return
	}
	fmt.Printf("  [%s] totalEquity=%s USD\n", accountType, r.List[0].TotalEquity)
	type row struct{ coin, wb, usd string }
	var rows []row
	for _, c := range r.List[0].Coin {
		wb, _ := strconv.ParseFloat(c.WalletBalance, 64)
		if wb == 0 {
			continue
		}
		rows = append(rows, row{c.Coin, c.WalletBalance, c.USDValue})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].coin < rows[j].coin })
	for _, x := range rows {
		fmt.Printf("      %-8s 잔고=%-16s (~%s USD)\n", x.coin, x.wb, x.usd)
	}
}

// fundBalance queries the Funding (FUND) account, which is separate from trading.
func fundBalance() {
	res, err := get("/v5/asset/transfer/query-account-coins-balance",
		url.Values{"accountType": {"FUND"}})
	if err != nil {
		fmt.Printf("  [FUND/펀딩] 조회 실패: %v\n", err)
		return
	}
	var r struct {
		Balance []struct {
			Coin          string `json:"coin"`
			WalletBalance string `json:"walletBalance"`
		} `json:"balance"`
	}
	json.Unmarshal(res, &r)
	any := false
	for _, c := range r.Balance {
		wb, _ := strconv.ParseFloat(c.WalletBalance, 64)
		if wb == 0 {
			continue
		}
		fmt.Printf("      %-8s 잔고=%s\n", c.Coin, c.WalletBalance)
		any = true
	}
	if !any {
		fmt.Println("      (펀딩계좌 잔고 0)")
	}
}

func main() {
	_ = godotenv.Load(".env")
	apiKey = os.Getenv("BYBIT_API_KEY")
	apiSecret = os.Getenv("BYBIT_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		fmt.Println("❌ .env에 키 없음")
		os.Exit(1)
	}

	fmt.Println("=== Bybit 전체 자산 조회 (조회 전용, 주문/이체 없음) ===")
	fmt.Println("\n■ UNIFIED (통합거래계좌 — 봇이 선물 증거금으로 쓰는 계좌):")
	walletBalance("UNIFIED")
	fmt.Println("\n■ FUND (펀딩계좌 — 입출금/현물 입금이 처음 들어오는 곳):")
	fundBalance()
}
