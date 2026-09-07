package ai

import "sync"

type budgetLedger struct {
	mu                      sync.Mutex
	budget                  ReviewBudget
	requests, input, output int
}

func newBudgetLedger(budget ReviewBudget) *budgetLedger {
	return &budgetLedger{budget: normalizeReviewBudget(budget)}
}

func (ledger *budgetLedger) reserve(input, output int) bool {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	if ledger.requests+1 > ledger.budget.MaxProviderRequests || ledger.input+input > ledger.budget.MaxEstimatedInputTokens || ledger.output+output > ledger.budget.MaxOutputTokens {
		return false
	}
	ledger.requests++
	ledger.input += input
	ledger.output += output
	return true
}

func (ledger *budgetLedger) usage() (int, int, int) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.requests, ledger.input, ledger.output
}
