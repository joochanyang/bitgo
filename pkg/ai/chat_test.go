package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallChatJSON(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"action\":\"HOLD\"}"}}]}`)
	}))
	defer srv.Close()

	c := NewAIClient()
	out, err := c.CallChatJSON(srv.URL, "test-key", "kimi-k2.6", "sys", "user")
	if err != nil {
		t.Fatalf("CallChatJSON: %v", err)
	}
	if out != `{"action":"HOLD"}` {
		t.Fatalf("unexpected content: %q", out)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth header wrong: %q", gotAuth)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req["model"] != "kimi-k2.6" {
		t.Fatalf("model wrong: %v", req["model"])
	}
}

func TestCallChatJSONHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"message":"suspended"}}`)
	}))
	defer srv.Close()

	c := NewAIClient()
	_, err := c.CallChatJSON(srv.URL, "k", "m", "s", "u")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

// 200이지만 choices가 비면 에러(빈 응답 방어).
func TestCallChatJSONEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	c := NewAIClient()
	_, err := c.CallChatJSON(srv.URL, "k", "m", "s", "u")
	if err == nil || !strings.Contains(err.Error(), "no completion choices") {
		t.Fatalf("expected no-choices error, got %v", err)
	}
}

// 200이지만 본문이 JSON이 아니면 에러(악성/손상 응답 방어).
func TestCallChatJSONBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `not json at all`)
	}))
	defer srv.Close()

	c := NewAIClient()
	_, err := c.CallChatJSON(srv.URL, "k", "m", "s", "u")
	if err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
}
