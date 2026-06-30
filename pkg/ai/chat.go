package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// CallChatJSON sends a chat-completions request to any OpenAI-compatible endpoint
// (OpenAI, Kimi/Moonshot, etc.) and returns the assistant message content. baseURL is
// the API root WITHOUT the path, e.g. "https://api.moonshot.ai/v1"; the standard
// "/chat/completions" path is appended. response_format is json_object so callers can
// parse a structured decision. Reuses the openaiRequest/openaiResponse shapes from ai.go.
func (ac *AIClient) CallChatJSON(baseURL, apiKey, model, systemPrompt, userPrompt string) (string, error) {
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

	url := baseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBytes))
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

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chat api returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var openResp openaiResponse
	if err := json.Unmarshal(respBytes, &openResp); err != nil {
		return "", err
	}
	if len(openResp.Choices) == 0 {
		return "", fmt.Errorf("no completion choices returned")
	}
	return openResp.Choices[0].Message.Content, nil
}
