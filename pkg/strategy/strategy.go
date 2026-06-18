package strategy

import (
	"go-bot/pkg/exchange"
)

// DecisionType represents the trading action decided by the strategy
type DecisionType string

const (
	LONG  DecisionType = "LONG"
	SHORT DecisionType = "SHORT"
	HOLD  DecisionType = "HOLD"
	CLOSE DecisionType = "CLOSE"
)

// Decision holds the structured output returned by a strategy
type Decision struct {
	Decision      DecisionType `json:"decision"`
	Leverage      int          `json:"leverage"`
	Confidence    float64      `json:"confidence"`
	TakeProfitPct float64      `json:"take_profit_pct"`
	StopLossPct   float64      `json:"stop_loss_pct"`
	Reasoning     string       `json:"reasoning"`
}

// Strategy defines the interface for executing quantitative trading strategies
type Strategy interface {
	Name() string
	Evaluate(symbol string, candles []exchange.Candle) (*Decision, error)
}
