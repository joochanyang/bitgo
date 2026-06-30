package main

import (
	"testing"
	"time"
)

func TestClassifyRegime(t *testing.T) {
	cases := []struct {
		price, low, high float64
		want             string
	}{
		{0.59, 0.50, 0.60, "trending_up"},   // 상단 10% 이내
		{0.51, 0.50, 0.60, "trending_down"}, // 하단 10% 이내
		{0.55, 0.50, 0.60, "ranging"},       // 가운데
		{0.5, 0.6, 0.6, "ranging"},          // high<=low 방어
	}
	for _, c := range cases {
		if got := classifyRegime(c.price, c.low, c.high); got != c.want {
			t.Errorf("classifyRegime(%v,%v,%v)=%s want %s", c.price, c.low, c.high, got, c.want)
		}
	}
}

func TestTickInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"4h", 4 * time.Hour},
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"15m", 15 * time.Minute},
		{"5m", 5 * time.Minute},
		{"bogus", 4 * time.Hour}, // 알 수 없으면 기본 4h
		{"", 4 * time.Hour},
	}
	for _, c := range cases {
		if got := tickInterval(c.in); got != c.want {
			t.Errorf("tickInterval(%q)=%v want %v", c.in, got, c.want)
		}
	}
}
