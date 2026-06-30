package main

import (
	"errors"
	"testing"

	"go-bot/pkg/exchange"
)

func TestBuildAccountBalanceFailureBlocks(t *testing.T) {
	ex := &stubExchange{balanceErr: errors.New("api down"), ticker: 0.5}
	acc, err := buildAccount(ex, "WLDUSDT", []string{"WLDUSDT"}, 3, 10.0)
	if err != nil {
		t.Fatalf("buildAccount should not hard-error on balance failure: %v", err)
	}
	if acc.BalanceOK {
		t.Fatal("BalanceOK should be false when GetBalance fails")
	}
}

func TestBuildAccountSumsCommittedRisk(t *testing.T) {
	// BTC has an open LONG with a stop -> contributes risk. WLD (the entry candidate)
	// must NOT count its own (it has no position here anyway).
	ex := &stubExchange{
		balance: 100, ticker: 0.5,
		positions: map[string]*exchange.Position{
			"BTCUSDT": {Symbol: "BTCUSDT", Side: "LONG", Size: 0.01, EntryPrice: 60000, StopLossPrice: 59000},
		},
	}
	acc, err := buildAccount(ex, "WLDUSDT", []string{"WLDUSDT", "BTCUSDT"}, 3, 10.0)
	if err != nil {
		t.Fatalf("buildAccount: %v", err)
	}
	if !acc.BalanceOK {
		t.Fatal("BalanceOK should be true")
	}
	// risk = size*|entry-sl| = 0.01*1000 = 10
	if acc.CommittedRiskUSDT < 9.99 || acc.CommittedRiskUSDT > 10.01 {
		t.Fatalf("CommittedRiskUSDT = %v, want ~10", acc.CommittedRiskUSDT)
	}
	if acc.Symbol != "WLDUSDT" || acc.Leverage != 3 || acc.MaxPortfolioRisk != 10.0 {
		t.Fatalf("account fields wrong: %+v", acc)
	}
}
