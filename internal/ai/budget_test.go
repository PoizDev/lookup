package ai

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestBudgetLedgerConcurrentReservationsCannotOverspend(t *testing.T) {
	ledger := newBudgetLedger(ReviewBudget{MaxReviewedFindings: 100, MaxProviderRequests: 3, MaxEstimatedInputTokens: 300, MaxOutputTokens: 300})
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if ledger.reserve(100, 100) {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("accepted = %d, want 3", accepted.Load())
	}
}
