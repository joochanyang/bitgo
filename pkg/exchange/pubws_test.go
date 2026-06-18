package exchange

import (
	"testing"
)

func TestPublicWSURL(t *testing.T) {
	if got := publicWSURL(false); got != "wss://stream.bybit.com/v5/public/linear" {
		t.Errorf("mainnet url = %q", got)
	}
	if got := publicWSURL(true); got != "wss://stream-testnet.bybit.com/v5/public/linear" {
		t.Errorf("testnet url = %q", got)
	}
}

func TestTickerTopic(t *testing.T) {
	if got := tickerTopic("BTCUSDT"); got != "tickers.BTCUSDT" {
		t.Errorf("tickerTopic = %q, want tickers.BTCUSDT", got)
	}
}

func TestBuildSubscribe(t *testing.T) {
	op := buildSubscribe([]string{"ETHUSDT", "BTCUSDT"})
	if op.Op != "subscribe" {
		t.Errorf("Op = %q, want subscribe", op.Op)
	}
	// Args must be sorted for determinism.
	want := []string{"tickers.BTCUSDT", "tickers.ETHUSDT"}
	if len(op.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", op.Args, want)
	}
	for i := range want {
		if op.Args[i] != want[i] {
			t.Errorf("Args[%d] = %q, want %q", i, op.Args[i], want[i])
		}
	}
}

func TestParseTickerMsg(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantSymbol string
		wantPrice  float64
		wantOK     bool
	}{
		{"snapshot with lastPrice", `{"topic":"tickers.BTCUSDT","type":"snapshot","data":{"symbol":"BTCUSDT","lastPrice":"27000.5"}}`, "BTCUSDT", 27000.5, true},
		{"delta empty lastPrice", `{"topic":"tickers.BTCUSDT","type":"delta","data":{"symbol":"BTCUSDT","lastPrice":""}}`, "", 0, false},
		{"wrong topic", `{"topic":"orderbook.50.BTCUSDT","type":"snapshot","data":{}}`, "", 0, false},
		{"subscribe ack", `{"success":true,"op":"subscribe"}`, "", 0, false},
		{"malformed json", `{not json`, "", 0, false},
		{"unparseable price", `{"topic":"tickers.BTCUSDT","type":"snapshot","data":{"symbol":"BTCUSDT","lastPrice":"abc"}}`, "", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sym, price, ok := parseTickerMsg([]byte(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && (sym != tc.wantSymbol || price != tc.wantPrice) {
				t.Errorf("got (%q, %v), want (%q, %v)", sym, price, tc.wantSymbol, tc.wantPrice)
			}
		})
	}
}
