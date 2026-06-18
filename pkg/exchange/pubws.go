package exchange

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// publicWSURL returns the Bybit public linear WebSocket endpoint, mirroring
// NewBybitExchange's testnet selection.
func publicWSURL(isTestnet bool) string {
	if isTestnet {
		return "wss://stream-testnet.bybit.com/v5/public/linear"
	}
	return "wss://stream.bybit.com/v5/public/linear"
}

// tickerTopic is the Bybit subscribe topic for a symbol's ticker stream.
func tickerTopic(symbol string) string { return "tickers." + symbol }

// wsOp is an outbound subscribe/unsubscribe/ping control frame.
type wsOp struct {
	Op   string   `json:"op"`
	Args []string `json:"args,omitempty"`
}

// buildSubscribe builds a subscribe op for the given symbols (sorted for determinism).
func buildSubscribe(symbols []string) wsOp {
	args := make([]string, 0, len(symbols))
	for _, s := range symbols {
		args = append(args, tickerTopic(s))
	}
	sort.Strings(args)
	return wsOp{Op: "subscribe", Args: args}
}

// wsTickerMsg is an inbound Bybit ticker push. Only snapshot frames are guaranteed
// to carry lastPrice; delta frames may omit it.
type wsTickerMsg struct {
	Topic string `json:"topic"`
	Data  struct {
		Symbol    string `json:"symbol"`
		LastPrice string `json:"lastPrice"`
	} `json:"data"`
}

// parseTickerMsg parses one raw WS frame into (symbol, price, ok). ok is false for
// non-ticker frames, acks, deltas without a price, or anything unparseable.
func parseTickerMsg(raw []byte) (string, float64, bool) {
	var m wsTickerMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", 0, false
	}
	if !strings.HasPrefix(m.Topic, "tickers.") || m.Data.LastPrice == "" {
		return "", 0, false
	}
	price, err := strconv.ParseFloat(m.Data.LastPrice, 64)
	if err != nil {
		return "", 0, false
	}
	return m.Data.Symbol, price, true
}

// PublicWS streams real-time public ticker prices from Bybit. It is used to tighten
// trailing stops between strategy ticks.
//
// SAFETY: the server-side hard stop-loss placed at position entry protects the
// position independently of this feed. A WS gap only pauses trailing tightening; it
// can never remove protection. Reconnect is therefore best-effort (capped backoff,
// no replay or buffering).
type PublicWS struct {
	wsURL   string
	onPrice func(symbol string, price float64)
}

// NewPublicWS creates a price feed. onPrice is called for every parsed tick and MUST
// be fast/non-blocking (the read loop calls it inline).
func NewPublicWS(isTestnet bool, onPrice func(symbol string, price float64)) *PublicWS {
	return &PublicWS{wsURL: publicWSURL(isTestnet), onPrice: onPrice}
}

// Run maintains one resilient connection subscribed to the given symbols' tickers
// until stop is closed. It reconnects with capped exponential backoff on any error.
// Integration-only (not unit-tested); the pure parse/build helpers carry the coverage.
func (w *PublicWS) Run(stop <-chan struct{}, symbols []string) {
	if len(symbols) == 0 {
		<-stop
		return
	}
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-stop:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(w.wsURL, nil)
		if err != nil {
			select {
			case <-time.After(backoff):
				backoff = nextBackoff(backoff, maxBackoff)
			case <-stop:
				return
			}
			continue
		}

		connectedAt := time.Now()
		w.pump(stop, conn, symbols)

		// Reset backoff if the connection stayed up a while; otherwise keep escalating.
		if time.Since(connectedAt) > time.Minute {
			backoff = time.Second
		} else {
			backoff = nextBackoff(backoff, maxBackoff)
		}

		select {
		case <-stop:
			return
		default:
		}
	}
}

// pump runs one connection's subscribe + ping + read loop until error or stop.
func (w *PublicWS) pump(stop <-chan struct{}, conn *websocket.Conn, symbols []string) {
	defer conn.Close()

	if err := conn.WriteJSON(buildSubscribe(symbols)); err != nil {
		return
	}

	done := make(chan struct{})
	defer close(done)

	// Ping heartbeat so the server keeps the connection alive.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteJSON(wsOp{Op: "ping"}); err != nil {
					return
				}
			case <-done:
				return
			case <-stop:
				return
			}
		}
	}()

	// Close the connection when stop fires so the blocking ReadMessage unblocks.
	go func() {
		select {
		case <-stop:
			conn.Close()
		case <-done:
		}
	}()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if sym, price, ok := parseTickerMsg(raw); ok {
			w.onPrice(sym, price)
		}
	}
}

// nextBackoff doubles the backoff up to a cap.
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}
