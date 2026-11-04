package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/platform"
)

func newTestRepository(t *testing.T) *TransferRepository {
	t.Helper()
	_ = os.Remove("wallet.db")
	db, err := platform.NewDatabase()
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		db.Close()
		_ = os.Remove("wallet.db")
		_ = os.Remove("wallet.db-shm")
		_ = os.Remove("wallet.db-wal")
	})
	return NewTransferRepository(db)
}

func TestTransferFundsUpdatesBalancesAndLedgerAtomically(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 500}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 100}); err != nil {
		t.Fatal(err)
	}

	transfer, err := repo.TransferFunds(ctx, &domain.Transfer{
		IdempotencyKey: "request-1", FromWalletID: "source", ToWalletID: "destination", Amount: 125,
	})
	if err != nil {
		t.Fatalf("transfer funds: %v", err)
	}
	if transfer.Status != domain.TransferStatusProcessed {
		t.Fatalf("status = %s, want PROCESSED", transfer.Status)
	}

	source, err := repo.GetWallet(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := repo.GetWallet(ctx, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 375 || destination.Balance != 225 {
		t.Fatalf("balances = %d/%d, want 375/225", source.Balance, destination.Balance)
	}

	debits, err := repo.ListLedgerEntries(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	credits, err := repo.ListLedgerEntries(ctx, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if len(debits) != 1 || debits[0].Type != domain.LedgerEntryDebit || len(credits) != 1 || credits[0].Type != domain.LedgerEntryCredit {
		t.Fatalf("unexpected ledger entries: debit=%+v credit=%+v", debits, credits)
	}
}

func TestTransferFundsIsIdempotentAndRejectsDifferentPayload(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 500})
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 0})
	request := &domain.Transfer{IdempotencyKey: "same-key", FromWalletID: "source", ToWalletID: "destination", Amount: 100}

	first, err := repo.TransferFunds(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.TransferFunds(ctx, &domain.Transfer{IdempotencyKey: "same-key", FromWalletID: "source", ToWalletID: "destination", Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("transfer IDs differ: %s != %s", first.ID, second.ID)
	}

	var recordCount int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM idempotency_records WHERE idempotency_key = ? AND transfer_id = ?`, "same-key", first.ID).Scan(&recordCount); err != nil {
		t.Fatalf("query idempotency record: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("idempotency record count = %d, want 1", recordCount)
	}

	_, err = repo.TransferFunds(ctx, &domain.Transfer{IdempotencyKey: "same-key", FromWalletID: "source", ToWalletID: "destination", Amount: 200})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want idempotency conflict", err)
	}
	wallet, err := repo.GetWallet(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 400 {
		t.Fatalf("source balance = %d, want 400", wallet.Balance)
	}
}

func TestTransferFundsDoesNotAllowOverdraft(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 50})
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 0})

	transfer, err := repo.TransferFunds(ctx, &domain.Transfer{IdempotencyKey: "too-large", FromWalletID: "source", ToWalletID: "destination", Amount: 75})
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("error = %v, want insufficient funds", err)
	}
	if transfer == nil || transfer.Status != domain.TransferStatusFailed {
		t.Fatalf("unexpected failed attempt: %+v", transfer)
	}
	var status, reason string
	if err := repo.db.QueryRowContext(ctx, `SELECT status, failure_reason FROM idempotency_records WHERE idempotency_key = ?`, "too-large").Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.IdempotencyStatusRetryableFailed) || reason != string(domain.IdempotencyFailureInsufficientFunds) {
		t.Fatalf("idempotency status/reason = %s/%s", status, reason)
	}
	source, _ := repo.GetWallet(ctx, "source")
	destination, _ := repo.GetWallet(ctx, "destination")
	if source.Balance != 50 || destination.Balance != 0 {
		t.Fatalf("balances changed after failed transfer: %d/%d", source.Balance, destination.Balance)
	}
}

func TestConcurrentTransfersCannotOverdrawWallet(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 100})
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 0})

	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := repo.TransferFunds(ctx, &domain.Transfer{IdempotencyKey: string(rune('a' + i)), FromWalletID: "source", ToWalletID: "destination", Amount: 75})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successful transfers = %d, want 1", successes)
	}
	source, err := repo.GetWallet(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := repo.GetWallet(ctx, "destination")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 25 || destination.Balance != 75 {
		t.Fatalf("balances = %d/%d, want 25/75", source.Balance, destination.Balance)
	}
}

func TestInsufficientFundsRetryWithSameKeyReevaluatesBalance(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 50})
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 0})
	request := &domain.Transfer{IdempotencyKey: "retry-key", FromWalletID: "source", ToWalletID: "destination", Amount: 75}

	_, err := repo.TransferFunds(ctx, request)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("first attempt error = %v", err)
	}
	if _, err := repo.db.ExecContext(ctx, `UPDATE wallets SET balance = 100 WHERE id = 'source'`); err != nil {
		t.Fatal(err)
	}

	completed, err := repo.TransferFunds(ctx, request)
	if err != nil {
		t.Fatalf("retry should succeed after funding wallet: %v", err)
	}
	if completed.Status != domain.TransferStatusProcessed || completed.ID == "" {
		t.Fatalf("unexpected completed transfer: %+v", completed)
	}
	var status string
	var reason sql.NullString
	var transferID string
	if err := repo.db.QueryRowContext(ctx, `SELECT status, failure_reason, transfer_id FROM idempotency_records WHERE idempotency_key = 'retry-key'`).Scan(&status, &reason, &transferID); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.IdempotencyStatusCompleted) || reason.Valid || transferID != completed.ID {
		t.Fatalf("unexpected idempotency record: %s %v %s", status, reason, transferID)
	}
	source, _ := repo.GetWallet(ctx, "source")
	destination, _ := repo.GetWallet(ctx, "destination")
	if source.Balance != 25 || destination.Balance != 75 {
		t.Fatalf("balances = %d/%d, want 25/75", source.Balance, destination.Balance)
	}
	entries, _ := repo.ListLedgerEntries(ctx, "source")
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want exactly one debit", len(entries))
	}
}

func TestConcurrentSameIdempotencyKeyCreatesOneTransfer(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "source", Balance: 200})
	_ = repo.CreateWallet(ctx, &domain.Wallet{ID: "destination", Balance: 0})

	const requests = 8
	var wg sync.WaitGroup
	results := make(chan *domain.Transfer, requests)
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transfer, err := repo.TransferFunds(ctx, &domain.Transfer{IdempotencyKey: "one-logical-request", FromWalletID: "source", ToWalletID: "destination", Amount: 75})
			results <- transfer
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var expectedID string
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent duplicate returned error: %v", err)
		}
	}
	for transfer := range results {
		if transfer == nil {
			t.Fatal("nil transfer")
		}
		if expectedID == "" {
			expectedID = transfer.ID
		} else if transfer.ID != expectedID {
			t.Fatalf("different transfer IDs: %s and %s", expectedID, transfer.ID)
		}
	}
	var transferCount, ledgerCount int
	_ = repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transfers`).Scan(&transferCount)
	_ = repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries`).Scan(&ledgerCount)
	if transferCount != 1 || ledgerCount != 2 {
		t.Fatalf("transfers/ledger = %d/%d, want 1/2", transferCount, ledgerCount)
	}
	source, _ := repo.GetWallet(ctx, "source")
	if source.Balance != 125 {
		t.Fatalf("source balance = %d, want 125", source.Balance)
	}
}
