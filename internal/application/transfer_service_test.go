package application

import (
	"context"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TestTransferService_Concurrency_DeadlockPrevention simulates the classic
// deadlock scenario: Two concurrent transfers between the same two wallets
// in opposite directions (A->B and B->A).
func TestTransferService_Concurrency_DeadlockPrevention(t *testing.T) {
	// Setup mocks and service
	// (Assume mock implementation that uses a mutex to simulate row-level locking)
	wr := &mockWalletRepoWithLocking{
		wallets: map[string]*domain.Wallet{
			"wallet-A": {ID: "wallet-A", Balance: 1000},
			"wallet-B": {ID: "wallet-B", Balance: 1000},
		},
	}
	service := NewTransferService(&mockTransactionManager{}, wr, &mockTransferRepo{}, nil, nil, nil)

	var wg sync.WaitGroup
	iterations := 100
	wg.Add(2)

	// Goroutine 1: Transfer A -> B
	go func() {

		defer wg.Done()
		for i := 0; i < iterations; i++ {
			req := TransferRequest{
				TransferID:   "t1",
				FromWalletID: "wallet-A",
				ToWalletID:   "wallet-B",
				Amount:       1,
			}
			_, _ = service.Execute(context.Background(), req)
		}
	}()

	// Goroutine 2: Transfer B -> A
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			req := TransferRequest{
				TransferID:   "t2",
				FromWalletID: "wallet-B",
				ToWalletID:   "wallet-A",
				Amount:       1,
			}
			_, _ = service.Execute(context.Background(), req)
		}
	}()

	wg.Wait()

	// If we reach here without a timeout or deadlock detection, the sorting worked.
	finalA, _ := wr.GetByID(context.Background(), "wallet-A")
	finalB, _ := wr.GetByID(context.Background(), "wallet-B")
	if finalA.Balance+finalB.Balance != 2000 {
		t.Errorf("Money was lost or created! Total: %d", finalA.Balance+finalB.Balance)
	}
}
