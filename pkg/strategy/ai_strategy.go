package strategy

import (
	"fmt"
	"go-bot/pkg/ai"
	"go-bot/pkg/exchange"
	"go-bot/pkg/indicators"
)

// AIStrategy wraps the LLM decision client into the strategy interface
type AIStrategy struct {
	ex exchange.Exchange
	ai *ai.AIClient
}

// NewAIStrategy creates a new instance of AIStrategy
func NewAIStrategy(ex exchange.Exchange, aiClient *ai.AIClient) *AIStrategy {
	return &AIStrategy{
		ex: ex,
		ai: aiClient,
	}
}

// Name returns the strategy identifier
func (s *AIStrategy) Name() string {
	return "ai"
}

// SetExchange rebinds the exchange the strategy reads balance/position from.
// Called when the engine hot-swaps backends (paper <-> live) so the AI prompt
// reflects the same account the orders execute against.
func (s *AIStrategy) SetExchange(ex exchange.Exchange) {
	s.ex = ex
}

// Evaluate uses the configured LLM API to analyze the market and make a decision
func (s *AIStrategy) Evaluate(symbol string, candles []exchange.Candle) (*Decision, error) {
	n := len(candles)
	if n < 200 { // Gemini/OpenAI strategy requires 200 candles for EMA200
		return &Decision{
			Decision:  HOLD,
			Leverage:  1,
			Reasoning: fmt.Sprintf("Insufficient historical data for AI Strategy: got %d, need at least 200 candles", n),
		}, nil
	}

	// Calculate indicators
	rsi, err := indicators.CalculateRSI(candles, 14)
	if err != nil {
		return nil, err
	}

	ema20, err := indicators.CalculateEMA(candles, 20)
	if err != nil {
		return nil, err
	}

	ema50, err := indicators.CalculateEMA(candles, 50)
	if err != nil {
		return nil, err
	}

	ema200, err := indicators.CalculateEMA(candles, 200)
	if err != nil {
		return nil, err
	}

	macdRes, err := indicators.CalculateMACD(candles, 12, 26, 9)
	if err != nil {
		return nil, err
	}

	// Fetch current position & balance from exchange
	balance, err := s.ex.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %v", err)
	}

	pos, err := s.ex.GetPosition(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch position: %v", err)
	}

	// Call AI client
	aiDec, err := s.ai.AnalyzeMarket(
		symbol, candles, rsi, ema20, ema50, ema200,
		macdRes.MACD, macdRes.Signal, macdRes.Histogram,
		balance, pos,
	)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %v", err)
	}

	return &Decision{
		Decision:      DecisionType(aiDec.Decision),
		Leverage:      aiDec.Leverage,
		Confidence:    aiDec.Confidence,
		TakeProfitPct: aiDec.TakeProfitPct,
		StopLossPct:   aiDec.StopLossPct,
		Reasoning:     aiDec.Reasoning,
	}, nil
}
