package main

import (
	"go-bot/pkg/agent"
	"go-bot/pkg/exchange"
	"go-bot/pkg/strategy"
)

// buildAccount snapshots the account state the guard checks an entry against. A balance
// lookup failure sets BalanceOK=false (guard blocks entries) rather than erroring the
// whole tick. CommittedRiskUSDT sums the stop-loss risk of OTHER symbols' open positions
// (the candidate symbol's own risk is not pre-committed). leverage and maxPortfolioRisk
// come from config.
func buildAccount(ex exchange.Exchange, symbol string, allSymbols []string, leverage int, maxPortfolioRisk float64) (agent.AccountState, error) {
	acc := agent.AccountState{
		Symbol:           symbol,
		Leverage:         leverage,
		MaxPortfolioRisk: maxPortfolioRisk,
	}

	bal, err := ex.GetBalance()
	if err != nil {
		acc.BalanceOK = false
	} else {
		acc.Balance = bal
		acc.BalanceOK = true
	}

	if price, err := ex.GetTicker(symbol); err == nil {
		acc.Price = price
	}

	// Sum committed risk from other symbols' open positions.
	var committed float64
	for _, sym := range allSymbols {
		if sym == symbol {
			continue
		}
		pos, err := ex.GetPosition(sym)
		if err != nil || pos == nil || pos.Side == "NONE" || pos.Size == 0 {
			continue
		}
		committed += strategy.PositionRiskUSDT(pos.Size, pos.EntryPrice, pos.StopLossPrice)
	}
	acc.CommittedRiskUSDT = committed

	return acc, nil
}
