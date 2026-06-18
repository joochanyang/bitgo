package exchange

import "time"

// Candle represents a single price bar (OHLCV)
type Candle struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume float64   `json:"volume"`
}

// Position represents an active futures position
type Position struct {
	Symbol          string  `json:"symbol"`
	Side            string  `json:"side"` // "LONG", "SHORT", or "NONE"
	Size            float64 `json:"size"`
	EntryPrice      float64 `json:"entry_price"`
	MarkPrice       float64 `json:"mark_price"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	Leverage        int     `json:"leverage"`
	StopLossPrice   float64 `json:"stop_loss_price,omitempty"`
	TakeProfitPrice float64 `json:"take_profit_price,omitempty"`
}

// OrderOptions holds advanced order settings like SL/TP
type OrderOptions struct {
	StopLossPrice   float64
	TakeProfitPrice float64
	ReduceOnly      bool
}

// OrderResult represents the response of a placed order
type OrderResult struct {
	OrderID      string    `json:"order_id"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"` // "Buy" or "Sell"
	Qty          float64   `json:"qty"`
	Price        float64   `json:"price"`
	Status       string    `json:"status"`
	TransactTime time.Time `json:"transact_time"`
}

// Exchange defines the interface for interacting with a crypto exchange
type Exchange interface {
	GetTicker(symbol string) (float64, error)
	GetKlines(symbol string, interval string, limit int) ([]Candle, error)
	GetKlinesPaged(symbol string, interval string, total int) ([]Candle, error)
	GetBalance() (float64, error)
	GetPosition(symbol string) (*Position, error)
	PlaceOrder(symbol string, side string, qty float64, price float64, opts OrderOptions) (*OrderResult, error)
	ClosePosition(symbol string) error
	SetLeverage(symbol string, leverage int) error
	SetStopLoss(symbol string, stopLossPrice float64) error
}
