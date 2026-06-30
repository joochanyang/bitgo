package main

import (
	"testing"

	"go-bot/pkg/agent/brain"
)

func TestPickCouncilDeepSeek(t *testing.T) {
	env := func(k string) string {
		if k == "DEEPSEEK_API_KEY" {
			return "sk-test"
		}
		return ""
	}
	c, label := pickCouncil(env)
	if label != "deepseek" {
		t.Fatalf("label = %q, want deepseek", label)
	}
	if _, ok := c.(*brain.LLMCouncil); !ok {
		t.Fatalf("expected *brain.LLMCouncil, got %T", c)
	}
}

func TestPickCouncilFallsBackToMock(t *testing.T) {
	env := func(string) string { return "" } // no key
	c, label := pickCouncil(env)
	if label != "mock" {
		t.Fatalf("label = %q, want mock", label)
	}
	if _, ok := c.(*brain.MockCouncil); !ok {
		t.Fatalf("expected *brain.MockCouncil, got %T", c)
	}
}
