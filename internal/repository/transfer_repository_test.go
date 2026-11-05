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
	if len(debits) != 2 || len(credits) != 2 {
		t.Fatalf("expected opening balance and transfer entries: debit wallet=%+v credit wallet=%+v", debits, credits)
	}
	var sourceDebit, sourceCredit, destinationCredits int
	for _, entry := range debits {
		if entry.Type == domain.LedgerEntryDebit {
			sourceDebit++
		}
		if entry.Type == domain.LedgerEntryCredit {
			sourceCredit++
		}
	}
	for _, entry := range credits {
		if entry.Type == domain.LedgerEntryCredit {
			destinationCredits++
		}
	}
	if sourceDebit != 1 || sourceCredit != 1 || destinationCredits != 2 {
		t.Fatalf("unexpected ledger entries: source=%+v destination=%+v", debits, credits)
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
	if _, err := repo.AddFunds(ctx, "source", 50); err != nil {
		t.Fatalf("add funds before retry: %v", err)
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
	var retryDebits, retryCredits int
	for _, entry := range entries {
		if entry.Type == domain.LedgerEntryDebit {
			retryDebits++
		}
		if entry.Type == domain.LedgerEntryCredit {
			retryCredits++
		}
	}
	if len(entries) != 3 || retryDebits != 1 || retryCredits != 2 {
		t.Fatalf("unexpected source ledger entries after funding and retry: %+v", entries)
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
	if transferCount != 1 || ledgerCount != 3 {
		t.Fatalf("transfers/ledger = %d/%d, want 1/3 including opening balance", transferCount, ledgerCount)
	}
	source, _ := repo.GetWallet(ctx, "source")
	if source.Balance != 125 {
		t.Fatalf("source balance = %d, want 125", source.Balance)
	}
}

func TestCreateWalletAndAddFundsRecordCredits(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "funded", Balance: 250}); err != nil {
		t.Fatal(err)
	}
	wallet, err := repo.GetWallet(ctx, "funded")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 250 || wallet.Status != domain.WalletStatusActive || wallet.CreatedAt == "" || wallet.UpdatedAt == "" {
		t.Fatalf("unexpected created wallet: %+v", wallet)
	}

	wallet, err = repo.AddFunds(ctx, "funded", 150)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 400 || wallet.UpdatedAt == "" {
		t.Fatalf("unexpected wallet after funding: %+v", wallet)
	}
	entries, err := repo.ListLedgerEntries(ctx, "funded")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Type != domain.LedgerEntryCredit || entries[1].Type != domain.LedgerEntryCredit {
		t.Fatalf("opening balance and funding should both be ledger credits: %+v", entries)
	}
	if entries[0].TransferID != "" || entries[1].TransferID != "" {
		t.Fatalf("wallet credits should not be linked to transfers: %+v", entries)
	}
}

func TestAddFundsRejectsInvalidAndInactiveWallets(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)

	if _, err := repo.AddFunds(ctx, "missing", 10); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("missing wallet error = %v", err)
	}
	if _, err := repo.AddFunds(ctx, "missing", 0); !errors.Is(err, domain.ErrInvalidWallet) {
		t.Fatalf("invalid amount error = %v", err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "inactive", Balance: 10, Status: domain.WalletStatusInactive}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AddFunds(ctx, "inactive", 10); !errors.Is(err, domain.ErrWalletInactive) {
		t.Fatalf("inactive wallet error = %v", err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "inactive-source", Balance: 100, Status: domain.WalletStatusInactive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "active-destination", Balance: 0}); err != nil {
		t.Fatal(err)
	}
	_, err := repo.TransferFunds(ctx, &domain.Transfer{
		IdempotencyKey: "inactive-wallet-transfer", FromWalletID: "inactive-source",
		ToWalletID: "active-destination", Amount: 10,
	})
	if !errors.Is(err, domain.ErrWalletInactive) {
		t.Fatalf("transfer from inactive wallet error = %v", err)
	}
}

func TestCreateWalletRejectsDuplicateAndNegativeBalance(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepository(t)
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "wallet", Balance: 10}); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "wallet", Balance: 20}); !errors.Is(err, domain.ErrWalletAlreadyExists) {
		t.Fatalf("duplicate wallet error = %v", err)
	}
	if err := repo.CreateWallet(ctx, &domain.Wallet{ID: "negative", Balance: -1}); !errors.Is(err, domain.ErrInvalidWallet) {
		t.Fatalf("negative balance error = %v", err)
	}
}
