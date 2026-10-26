package service_test

import (
	"context"
	"math"
	"testing"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

// Regression for BIGINT credit overflow after the sender has already been
// debited. PostgreSQL must abort the full transaction, including the unique
// idempotency claim. Funding capacity is later released via a real transfer,
// and retrying the original key must then succeed.
func TestPostgresFinancialBoundaries(t *testing.T) {
	t.Run("credit overflow rolls back financial writes and idempotency claim", func(t *testing.T) {
		db := testDatabase(t)
		seedWallets(t, db, map[string]int64{"A": 1, "B": math.MaxInt64, "C": 0})
		svc := service.New(repository.NewPostgres(db))
		overflowing := domain.TransferRequest{
			IdempotencyKey: "credit-overflow", FromWalletID: "A", ToWalletID: "B", Amount: 1,
		}

		result, err := svc.Transfer(context.Background(), overflowing)
		if err == nil || result != (domain.TransferResult{}) {
			t.Fatalf("overflow should abort: result=%+v err=%v", result, err)
		}
		assertBalance(t, db, "A", 1)
		assertBalance(t, db, "B", math.MaxInt64)
		assertBalance(t, db, "C", 0)
		assertTransferKeyCount(t, db, overflowing.IdempotencyKey, 0)
		assertTransferCount(t, db, 0)
		assertTotalLedgerCount(t, db, 0)
		assertReconciles(t, db)

		// Free exactly one minor unit of capacity in B without altering the
		// opening balances or bypassing the ordinary ledger workflow.
		capacity := domain.TransferRequest{
			IdempotencyKey: "release-capacity", FromWalletID: "B", ToWalletID: "C", Amount: 1,
		}
		moved, err := svc.Transfer(context.Background(), capacity)
		if err != nil || moved.Status != domain.StatusProcessed {
			t.Fatalf("capacity release: result=%+v err=%v", moved, err)
		}
		recovered, err := svc.Transfer(context.Background(), overflowing)
		if err != nil || recovered.Status != domain.StatusProcessed {
			t.Fatalf("retry original key: result=%+v err=%v", recovered, err)
		}
		assertBalance(t, db, "A", 0)
		assertBalance(t, db, "B", math.MaxInt64)
		assertBalance(t, db, "C", 1)
		assertStoredTransfer(t, db, moved.ID, capacity, domain.StatusProcessed, "")
		assertStoredTransfer(t, db, recovered.ID, overflowing, domain.StatusProcessed, "")
		assertLedger(t, db, moved.ID, []ledgerExpectation{
			{wallet: "B", kind: "DEBIT", amount: 1},
			{wallet: "C", kind: "CREDIT", amount: 1},
		})
		assertLedger(t, db, recovered.ID, []ledgerExpectation{
			{wallet: "A", kind: "DEBIT", amount: 1},
			{wallet: "B", kind: "CREDIT", amount: 1},
		})
		assertTransferCount(t, db, 2)
		assertTotalLedgerCount(t, db, 4)
		assertReconciles(t, db)
	})
}
