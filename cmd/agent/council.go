package main

import (
	"go-bot/pkg/agent"
	"go-bot/pkg/agent/brain"
)

// DeepSeek defaults (OpenAI-compatible). Overridable via env DEEPSEEK_BASE_URL/DEEPSEEK_MODEL.
const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com/v1"
	defaultDeepSeekModel   = "deepseek-v4-flash"
)

// pickCouncil chooses the council from the environment: a real DeepSeek-backed LLMCouncil
// when DEEPSEEK_API_KEY is set, otherwise a MockCouncil that always HOLDs (zero cost, safe
// for wiring/dry runs). env is injected so the choice is unit-testable. Returns the council
// and a short label for the startup log.
func pickCouncil(env func(string) string) (brain.Council, string) {
	key := env("DEEPSEEK_API_KEY")
	if key == "" {
		return brain.NewMockCouncil(agent.Decision{Action: agent.ActionHold, Reasoning: "no DEEPSEEK_API_KEY: mock council"}), "mock"
	}
	baseURL := env("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	model := env("DEEPSEEK_MODEL")
	if model == "" {
		model = defaultDeepSeekModel
	}
	llm := brain.NewOpenAICompatLLM(baseURL, key, model)
	return brain.NewLLMCouncil(llm), "deepseek"
}
