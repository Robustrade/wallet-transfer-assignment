//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/model"
	"wallet-transfer-assignment/internal/feature/wallet/repository"

	"gorm.io/gorm"
)

// Repository behaviour against real PostgreSQL. testDB is used directly as the
// transaction handle (each statement auto-commits).

func newTransfer(t *testing.T, from, to, amount int64) *domain.Transfer {
	t.Helper()
	tr := &domain.Transfer{IdempotencyKey: uniqueKey(t, 0), FromWalletID: from, ToWalletID: to, Amount: amount}
	created, err := repository.NewTransferRepository().Create(context.Background(), testDB, tr)
	if err != nil || !created {
		t.Fatalf("create transfer: created=%v err=%v", created, err)
	}
	return tr
}

// ---- wallets -------------------------------------------------------------

func TestWalletRepository_ReadsAndLocksExistingWallets(t *testing.T) {
	repo := repository.NewWalletRepository()
	id := newWallet(t, 250)

	got, err := repo.GetByID(context.Background(), testDB, id)
	if err != nil || got.ID != id || got.Balance != 250 {
		t.Fatalf("GetByID: %+v %v", got, err)
	}

	locked, err := repo.GetByIDForUpdate(context.Background(), testDB, id)
	if err != nil || locked.ID != id || locked.Balance != 250 {
		t.Fatalf("GetByIDForUpdate: %+v %v", locked, err)
	}
}

func TestWalletRepository_UnknownWalletIsReportedAsNotFound(t *testing.T) {
	repo := repository.NewWalletRepository()
	const missing = int64(9_000_000_000)

	if _, err := repo.GetByID(context.Background(), testDB, missing); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Errorf("GetByID: %v", err)
	}
	if _, err := repo.GetByIDForUpdate(context.Background(), testDB, missing); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Errorf("GetByIDForUpdate: %v", err)
	}
	if err := repo.UpdateBalance(context.Background(), testDB, missing, 10); err == nil {
		t.Error("UpdateBalance on a missing wallet must fail")
	}
}

func TestWalletRepository_UpdateBalanceAppliesDeltasAndNeverGoesNegative(t *testing.T) {
	repo := repository.NewWalletRepository()
	id := newWallet(t, 100)

	if err := repo.UpdateBalance(context.Background(), testDB, id, 50); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateBalance(context.Background(), testDB, id, -120); err != nil {
		t.Fatal(err)
	}
	if got := balance(t, id); got != 30 {
		t.Fatalf("balance = %d, want 30", got)
	}

	if err := repo.UpdateBalance(context.Background(), testDB, id, -31); err == nil {
		t.Fatal("overdraw must be rejected by the CHECK constraint")
	}
	if got := balance(t, id); got != 30 {
		t.Fatalf("rejected update changed the balance to %d", got)
	}
}

// ---- transfers -----------------------------------------------------------

func TestTransferRepository_CreateClaimsTheKeyOnlyOnce(t *testing.T) {
	repo := repository.NewTransferRepository()
	from, to := newWallet(t, 100), newWallet(t, 0)
	first := newTransfer(t, from, to, 10)

	if first.ID <= 0 || first.Status != domain.TransferPending {
		t.Fatalf("created transfer = %+v", first)
	}

	dup := &domain.Transfer{IdempotencyKey: first.IdempotencyKey, FromWalletID: from, ToWalletID: to, Amount: 10}
	created, err := repo.Create(context.Background(), testDB, dup)
	if err != nil || created {
		t.Fatalf("duplicate key: created=%v err=%v", created, err)
	}
	if dup.ID != 0 {
		t.Errorf("duplicate must not receive an id, got %d", dup.ID)
	}
}

func TestTransferRepository_CreateRejectsUnknownWallets(t *testing.T) {
	tr := &domain.Transfer{IdempotencyKey: uniqueKey(t, 0), FromWalletID: newWallet(t, 1), ToWalletID: 9_000_000_000, Amount: 1}

	created, err := repository.NewTransferRepository().Create(context.Background(), testDB, tr)
	if err == nil || created {
		t.Fatalf("foreign key violation expected: created=%v err=%v", created, err)
	}
}

func TestTransferRepository_GetByIdempotencyKey(t *testing.T) {
	repo := repository.NewTransferRepository()
	tr := newTransfer(t, newWallet(t, 100), newWallet(t, 0), 25)

	got, err := repo.GetByIdempotencyKey(context.Background(), testDB, tr.IdempotencyKey)
	if err != nil || got.ID != tr.ID || got.Amount != 25 || got.Status != domain.TransferPending {
		t.Fatalf("got %+v err=%v", got, err)
	}

	_, err = repo.GetByIdempotencyKey(context.Background(), testDB, "no-such-key-1")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("got %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestTransferRepository_UpdateStatusIsGuardedAndOnlyMovesOutOfPending(t *testing.T) {
	repo := repository.NewTransferRepository()
	tr := newTransfer(t, newWallet(t, 100), newWallet(t, 0), 10)

	reason := "INSUFFICIENT_FUNDS"
	failed := &domain.Transfer{ID: tr.ID, Status: domain.TransferFailed, FailureReason: &reason}
	if err := repo.UpdateStatus(context.Background(), testDB, failed); err != nil {
		t.Fatal(err)
	}

	got, _ := repo.GetByIdempotencyKey(context.Background(), testDB, tr.IdempotencyKey)
	if got.Status != domain.TransferFailed || got.FailureReason == nil || *got.FailureReason != reason {
		t.Fatalf("stored transfer = %+v", got)
	}

	// Terminal states cannot be changed again, even by a direct repository call.
	processed := &domain.Transfer{ID: tr.ID, Status: domain.TransferProcessed}
	if err := repo.UpdateStatus(context.Background(), testDB, processed); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("second update: %v, want ErrInvalidTransition", err)
	}
	got, _ = repo.GetByIdempotencyKey(context.Background(), testDB, tr.IdempotencyKey)
	if got.Status != domain.TransferFailed {
		t.Fatalf("status overwritten: %s", got.Status)
	}
}

func TestTransferRepository_UpdateStatusOfUnknownTransferFails(t *testing.T) {
	err := repository.NewTransferRepository().UpdateStatus(context.Background(), testDB,
		&domain.Transfer{ID: 9_000_000_000, Status: domain.TransferProcessed})
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("got %v", err)
	}
}

// ---- ledger --------------------------------------------------------------

func TestLedgerRepository_StoresAndReadsBackTheDebitCreditPair(t *testing.T) {
	repo := repository.NewLedgerRepository()
	from, to := newWallet(t, 100), newWallet(t, 0)
	tr := newTransfer(t, from, to, 40)

	entries, err := domain.NewLedgerEntries(tr.ID, from, to, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateEntries(context.Background(), testDB, entries); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByTransferID(context.Background(), testDB, tr.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("entries=%+v err=%v", got, err)
	}
	if got[0].EntryType != domain.LedgerDebit || got[0].WalletID != from || got[0].Amount != 40 ||
		got[1].EntryType != domain.LedgerCredit || got[1].WalletID != to || got[1].Amount != 40 {
		t.Errorf("entries = %+v", got)
	}

	none, err := repo.GetByTransferID(context.Background(), testDB, 9_000_000_000)
	if err != nil || len(none) != 0 {
		t.Errorf("unknown transfer: %+v %v", none, err)
	}
}

func TestLedgerRepository_RejectsASecondPairForTheSameTransfer(t *testing.T) {
	repo := repository.NewLedgerRepository()
	from, to := newWallet(t, 100), newWallet(t, 0)
	tr := newTransfer(t, from, to, 5)
	entries, _ := domain.NewLedgerEntries(tr.ID, from, to, 5)

	if err := repo.CreateEntries(context.Background(), testDB, entries); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateEntries(context.Background(), testDB, entries); err == nil {
		t.Fatal("a second debit/credit pair must violate uq_ledger_transfer_entry_type")
	}
	if n := count(t, &model.LedgerEntry{}, "transfer_id = ?", tr.ID); n != 2 {
		t.Fatalf("%d ledger rows, want 2", n)
	}
}

func TestLedgerRepository_RejectsEntriesForAMissingTransfer(t *testing.T) {
	from, to := newWallet(t, 1), newWallet(t, 1)
	entries, _ := domain.NewLedgerEntries(9_000_000_000, from, to, 1)

	if err := repository.NewLedgerRepository().CreateEntries(context.Background(), testDB, entries); err == nil {
		t.Fatal("ledger rows without a transfer must violate the foreign key")
	}
}

// ---- idempotency records -------------------------------------------------

func TestIdempotencyRepository_CreateClaimsTheKeyOnlyOnce(t *testing.T) {
	repo := repository.NewIdempotencyRepository()
	key := uniqueKey(t, 0)

	first := &domain.IdempotencyRecord{IdempotencyKey: key, RequestHash: "hash-1"}
	created, err := repo.Create(context.Background(), testDB, first)
	if err != nil || !created || first.ID <= 0 {
		t.Fatalf("first: created=%v id=%d err=%v", created, first.ID, err)
	}

	second := &domain.IdempotencyRecord{IdempotencyKey: key, RequestHash: "hash-2"}
	created, err = repo.Create(context.Background(), testDB, second)
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}

	got, err := repo.GetByKey(context.Background(), testDB, key)
	if err != nil || got.RequestHash != "hash-1" || got.ResponseStatus != nil {
		t.Fatalf("stored record = %+v err=%v", got, err)
	}
}

func TestIdempotencyRepository_GetByKeyReportsMissingKeys(t *testing.T) {
	_, err := repository.NewIdempotencyRepository().GetByKey(context.Background(), testDB, "no-such-key-1")
	if !errors.Is(err, domain.ErrIdempotencyNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestIdempotencyRepository_SaveResponseStoresItExactlyOnce(t *testing.T) {
	repo := repository.NewIdempotencyRepository()
	tr := newTransfer(t, newWallet(t, 10), newWallet(t, 0), 1)
	rec := &domain.IdempotencyRecord{IdempotencyKey: uniqueKey(t, 1), RequestHash: "hash"}
	if _, err := repo.Create(context.Background(), testDB, rec); err != nil {
		t.Fatal(err)
	}

	status := 201
	rec.TransferID = &tr.ID
	rec.ResponseStatus = &status
	rec.ResponseBody = []byte(`{"status":"PROCESSED"}`)
	if err := repo.SaveResponse(context.Background(), testDB, rec); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetByKey(context.Background(), testDB, rec.IdempotencyKey)
	if err != nil || got.ResponseStatus == nil || *got.ResponseStatus != 201 ||
		got.TransferID == nil || *got.TransferID != tr.ID || len(got.ResponseBody) == 0 {
		t.Fatalf("stored record = %+v err=%v", got, err)
	}

	// The first stored response wins: it can never be overwritten.
	other := 409
	rec.ResponseStatus = &other
	if err := repo.SaveResponse(context.Background(), testDB, rec); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("second save: %v, want ErrIdempotencyConflict", err)
	}
	got, _ = repo.GetByKey(context.Background(), testDB, rec.IdempotencyKey)
	if *got.ResponseStatus != 201 {
		t.Fatalf("response overwritten with %d", *got.ResponseStatus)
	}
}

// ---- transaction manager -------------------------------------------------

func TestTransactionManager_CommitsOnSuccessAndRollsBackOnError(t *testing.T) {
	mgr := repository.NewTransactionManager(testDB)
	repo := repository.NewWalletRepository()
	id := newWallet(t, 100)

	failure := errors.New("business failure")
	err := mgr.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		if err := repo.UpdateBalance(context.Background(), tx, id, -40); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("got %v", err)
	}
	if got := balance(t, id); got != 100 {
		t.Fatalf("rolled-back change is visible: balance %d", got)
	}

	err = mgr.WithinTransaction(context.Background(), func(tx *gorm.DB) error {
		return repo.UpdateBalance(context.Background(), tx, id, -40)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := balance(t, id); got != 60 {
		t.Fatalf("committed change missing: balance %d", got)
	}
}

func TestTransactionManager_CancelledContextStartsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	err := repository.NewTransactionManager(testDB).WithinTransaction(ctx, func(*gorm.DB) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}
