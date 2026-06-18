package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"go-bot/pkg/config"
	"go-bot/pkg/exchange"
)

// DecisionType represents the trading action decided by the AI
type DecisionType string

const (
	LONG  DecisionType = "LONG"
	SHORT DecisionType = "SHORT"
	HOLD  DecisionType = "HOLD"
	CLOSE DecisionType = "CLOSE"
)

// AIDecision holds the structured output returned by the LLM
type AIDecision struct {
	Decision      DecisionType `json:"decision"`
	Leverage      int          `json:"leverage"`
	Confidence    float64      `json:"confidence"`
	TakeProfitPct float64      `json:"take_profit_pct"`
	StopLossPct   float64      `json:"stop_loss_pct"`
	Reasoning     string       `json:"reasoning"`
}

// AIClient manages connections to LLM providers
type AIClient struct {
	client *http.Client
}

// NewAIClient creates a new client for calling AI APIs
func NewAIClient() *AIClient {
	return &AIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// AnalyzeMarket builds the prompt and queries the configured AI provider
func (ac *AIClient) AnalyzeMarket(symbol string, candles []exchange.Candle, rsi []float64, ema20, ema50, ema200 []float64, macdLine, signalLine, histogram []float64, balance float64, pos *exchange.Position) (*AIDecision, error) {
	cfg := config.GetConfig()

	// Prepare data summary to reduce token usage
	marketContext := ac.buildMarketContext(symbol, candles, rsi, ema20, ema50, ema200, macdLine, signalLine, histogram)
	portfolioContext := ac.buildPortfolioContext(balance, pos)

	systemPrompt := `You are an expert quantitative crypto futures trader. Your goal is to maximize profits while strictly managing risk.
You will be provided with:
1. Technical Indicators for a specific trading pair (recent candles, EMA, RSI, MACD).
2. Current Portfolio Status (total USDT balance, active position details).

You must analyze the trend, momentum, support/resistance, and volatility, then make a trading decision.
Your decision must be one of:
- "LONG": Open a long position (or add to it) if you see a strong upward trend or bullish reversal.
- "SHORT": Open a short position (or add to it) if you see a strong downward trend or bearish reversal.
- "HOLD": Do nothing, maintain current state.
- "CLOSE": Close the current active position immediately due to trend reversal, stop loss/take profit triggers, or high risk.

You MUST respond with a single, valid JSON object containing exactly the following schema. Do NOT include markdown styling, formatting, or extra text.

JSON Schema:
{
  "decision": "LONG" | "SHORT" | "HOLD" | "CLOSE",
  "leverage": integer (1 to 5),
  "confidence": float (0.0 to 1.0),
  "take_profit_pct": float (0.5 to 10.0),
  "stop_loss_pct": float (0.5 to 5.0),
  "reasoning": "concise explanation of your technical analysis, support/resistance levels, and decision details"
}`

	userPrompt := fmt.Sprintf("PORTFOLIO STATUS:\n%s\n\nMARKET DATA CONTEXT:\n%s\n\nAnalyze and output your JSON decision:", portfolioContext, marketContext)

	var responseText string
	var err error

	if cfg.AIProvider == "openai" {
		if cfg.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("OpenAI API Key is missing in configuration")
		}
		responseText, err = ac.callOpenAI(cfg.OpenAIAPIKey, cfg.OpenAIModel, systemPrompt, userPrompt)
	} else {
		// Default to gemini
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("Gemini API Key is missing in configuration")
		}
		responseText, err = ac.callGemini(cfg.GeminiAPIKey, cfg.GeminiModel, systemPrompt, userPrompt)
	}

	if err != nil {
		return nil, fmt.Errorf("AI API call failed: %v", err)
	}

	// Parse JSON output
	var decision AIDecision
	if err := json.Unmarshal([]byte(responseText), &decision); err != nil {
		// If unmarshal fails directly, attempt to strip potential markdown codeblocks ```json ... ```
		cleanJSON := ac.stripMarkdown(responseText)
		if err := json.Unmarshal([]byte(cleanJSON), &decision); err != nil {
			return nil, fmt.Errorf("failed to parse AI decision JSON: %v, raw text: %s", err, responseText)
		}
	}

	sanitizeDecision(&decision)
	return &decision, nil
}

// Explain asks the configured LLM to describe the current situation in plain Korean
// for a beginner. Unlike AnalyzeMarket it returns free-form text (no JSON/decision),
// so it never feeds an order — it is purely an on-demand "what's going on?" narration.
// Returns an error (surfaced to the user as guidance) when no API key is configured.
func (ac *AIClient) Explain(userPrompt string) (string, error) {
	cfg := config.GetConfig()

	systemPrompt := `당신은 암호화폐 선물 자동매매 봇의 상황을 초보자에게 설명하는 친절한 도우미입니다.
주어진 시장/포지션 정보를 바탕으로, 전문용어를 최대한 풀어서 한국어로 2~4문장으로 설명하세요.
- 지금 봇이 무엇을 하고 있는지(대기/보유/수익/손실)
- 왜 그런지(쉬운 말로)
- 초보가 알아야 할 핵심 한 가지
투자 조언이나 단정적 예측은 하지 말고, 현재 상태를 사실 그대로 쉽게 풀어주세요. 마크다운 없이 평문으로만 답하세요.`

	var responseText string
	var err error
	if cfg.AIProvider == "openai" {
		if cfg.OpenAIAPIKey == "" {
			return "", fmt.Errorf("OpenAI API 키가 설정되지 않았습니다. 설정에서 키를 입력하면 AI 해설을 쓸 수 있어요.")
		}
		responseText, err = ac.callOpenAI(cfg.OpenAIAPIKey, cfg.OpenAIModel, systemPrompt, userPrompt)
	} else {
		if cfg.GeminiAPIKey == "" {
			return "", fmt.Errorf("Gemini API 키가 설정되지 않았습니다. 설정에서 키를 입력하면 AI 해설을 쓸 수 있어요.")
		}
		responseText, err = ac.callGemini(cfg.GeminiAPIKey, cfg.GeminiModel, systemPrompt, userPrompt)
	}
	if err != nil {
		return "", fmt.Errorf("AI 해설 호출 실패: %v", err)
	}
	return ac.stripMarkdown(responseText), nil
}

// sanitizeDecision validates and clamps an AI decision so a hallucinated action or an
// out-of-range SL/TP/leverage cannot flow into a real order. Unknown actions become HOLD.
func sanitizeDecision(decision *AIDecision) {
	// Validate the decision is one of the known actions; anything else is treated as HOLD
	// to prevent a hallucinated/garbled value from falling through to an unintended order.
	switch decision.Decision {
	case LONG, SHORT, HOLD, CLOSE:
		// valid
	default:
		decision.Reasoning = fmt.Sprintf("[REJECTED unknown decision %q -> HOLD] %s", decision.Decision, decision.Reasoning)
		decision.Decision = HOLD
	}

	// Clamp leverage for safety.
	if decision.Leverage < 1 {
		decision.Leverage = 1
	} else if decision.Leverage > 5 {
		decision.Leverage = 5 // Hard ceiling
	}

	// Clamp stop-loss / take-profit percentages to the documented ranges so a 0/negative/absurd
	// value cannot place an SL at (or the wrong side of) the entry price.
	if decision.StopLossPct < 0.5 {
		decision.StopLossPct = 0.5
	} else if decision.StopLossPct > 5.0 {
		decision.StopLossPct = 5.0
	}
	if decision.TakeProfitPct < 0.5 {
		decision.TakeProfitPct = 0.5
	} else if decision.TakeProfitPct > 10.0 {
		decision.TakeProfitPct = 10.0
	}
}

// buildMarketContext creates a text description of recent candles and indicator values
func (ac *AIClient) buildMarketContext(symbol string, candles []exchange.Candle, rsi []float64, ema20, ema50, ema200 []float64, macdLine, signalLine, histogram []float64) string {
	n := len(candles)
	if n == 0 {
		return "No market data available"
	}

	// Detail the last 5 candles
	var sb bytes.Buffer
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", symbol))
	sb.WriteString("Recent Candles (Open, High, Low, Close, Volume):\n")

	startIdx := n - 5
	if startIdx < 0 {
		startIdx = 0
	}

	for i := startIdx; i < n; i++ {
		c := candles[i]
		sb.WriteString(fmt.Sprintf("- Time: %s, O: %.4f, H: %.4f, L: %.4f, C: %.4f, Vol: %.1f\n",
			c.Time.Format("2006-01-02 15:04"), c.Open, c.High, c.Low, c.Close, c.Volume))
	}

	// Add latest indicators
	latestIdx := n - 1
	sb.WriteString("\nLatest Indicators:\n")

	if latestIdx < len(rsi) && rsi[latestIdx] > 0 {
		sb.WriteString(fmt.Sprintf("- RSI(14): %.2f\n", rsi[latestIdx]))
	}
	if latestIdx < len(ema20) && ema20[latestIdx] > 0 {
		sb.WriteString(fmt.Sprintf("- EMA(20): %.4f\n", ema20[latestIdx]))
	}
	if latestIdx < len(ema50) && ema50[latestIdx] > 0 {
		sb.WriteString(fmt.Sprintf("- EMA(50): %.4f\n", ema50[latestIdx]))
	}
	if latestIdx < len(ema200) && ema200[latestIdx] > 0 {
		sb.WriteString(fmt.Sprintf("- EMA(200): %.4f\n", ema200[latestIdx]))
	}
	if latestIdx < len(macdLine) && macdLine[latestIdx] != 0 {
		sb.WriteString(fmt.Sprintf("- MACD Line: %.4f, Signal: %.4f, Histogram: %.4f\n",
			macdLine[latestIdx], signalLine[latestIdx], histogram[latestIdx]))
	}

	// Trend description
	if latestIdx >= 1 {
		currPrice := candles[latestIdx].Close
		prevPrice := candles[latestIdx-1].Close
		priceChange := ((currPrice - prevPrice) / prevPrice) * 100
		sb.WriteString(fmt.Sprintf("- Price change in last bar: %.2f%%\n", priceChange))
	}

	return sb.String()
}

// buildPortfolioContext creates a text description of active positions and balance
func (ac *AIClient) buildPortfolioContext(balance float64, pos *exchange.Position) string {
	if pos == nil || pos.Side == "NONE" || pos.Size == 0 {
		return fmt.Sprintf("- Total Available USDT Balance: %.2f USDT\n- Active Position: NONE\n", balance)
	}

	return fmt.Sprintf("- Total Available USDT Balance: %.2f USDT\n- Active Position:\n  * Symbol: %s\n  * Side: %s\n  * Size: %.3f\n  * Entry Price: %.4f\n  * Mark Price: %.4f\n  * Unrealized PnL: %.2f USDT (%.2f%%)\n  * Leverage: %dx\n",
		balance, pos.Symbol, pos.Side, pos.Size, pos.EntryPrice, pos.MarkPrice, pos.UnrealizedPnL, (pos.UnrealizedPnL/(pos.EntryPrice*pos.Size))*100, pos.Leverage)
}

// OpenAI API Client implementation
type openaiRequest struct {
	Model          string          `json:"model"`
	Messages       []openaiMessage `json:"messages"`
	ResponseFormat interface{}     `json:"response_format"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (ac *AIClient) callOpenAI(apiKey, model, systemPrompt, userPrompt string) (string, error) {
	reqBody := openaiRequest{
		Model: model,
		Messages: []openaiMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(reqBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := ac.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var openResp openaiResponse
	if err := json.Unmarshal(respBytes, &openResp); err != nil {
		return "", err
	}

	if len(openResp.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned by OpenAI")
	}

	return openResp.Choices[0].Message.Content, nil
}

// Gemini API Client implementation
type geminiRequest struct {
	Contents          []geminiContent    `json:"contents"`
	SystemInstruction *geminiInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiConfig      `json:"generationConfig,omitempty"`
}

type geminiInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiConfig struct {
	ResponseMimeType string `json:"responseMimeType"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func (ac *AIClient) callGemini(apiKey, model, systemPrompt, userPrompt string) (string, error) {
	// Format request payload for Gemini V1beta API
	reqBody := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: userPrompt},
				},
			},
		},
		SystemInstruction: &geminiInstruction{
			Parts: []geminiPart{
				{Text: systemPrompt},
			},
		},
		GenerationConfig: &geminiConfig{
			ResponseMimeType: "application/json",
		},
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	// Call the Gemini API endpoint
	apiURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := ac.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(respBytes, &geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no text returned in Gemini response candidates")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// Helper to strip markdown block wrappers
func (ac *AIClient) stripMarkdown(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "```") {
		// Strip start ```json or ```
		lines := strings.Split(input, "\n")
		if len(lines) > 2 {
			if strings.HasPrefix(lines[0], "```") {
				lines = lines[1:]
			}
			if strings.HasSuffix(lines[len(lines)-1], "```") {
				lines = lines[:len(lines)-1]
			}
			return strings.Join(lines, "\n")
		}
	}
	return input
}
