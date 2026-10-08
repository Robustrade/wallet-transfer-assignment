package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

func TestRestoreTransfer(t *testing.T) {
	created := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	updated := created.Add(time.Minute)
	for _, status := range []domain.TransferStatus{domain.TransferStatusPending, domain.TransferStatusProcessed, domain.TransferStatusFailed} {
		v, err := restoreTransfer("t", "key", "b", "a", 42, status, created, updated)
		if err != nil {
			t.Fatal(err)
		}
		if v.ID != "t" || v.IdempotencyKey != "key" || v.FromWalletID != "b" || v.ToWalletID != "a" || v.Amount != 42 || v.Status() != status || v.CreatedAt != created || v.UpdatedAt != updated {
			t.Fatalf("wrong reconstruction: %+v", v)
		}
		if status != domain.TransferStatusPending && v.MarkProcessed(updated) == nil {
			t.Fatal("terminal state became mutable")
		}
	}
	if _, err := restoreTransfer("t", "", "a", "b", 1, "BAD", created, updated); err == nil {
		t.Fatal("invalid stored status accepted")
	}
}

func TestDatabaseFailureStopsFurtherOperations(t *testing.T) {
	// No SQL handle is needed: a failed adapter must reject calls before using it.
	tx := &transaction{}
	first := tx.record("insert", &pgconn.PgError{Code: "23505"})
	if !errors.Is(first, repository.ErrConflict) || !errors.Is(first, repository.ErrTransactionFailed) {
		t.Fatal("unique violation must require rollback")
	}
	if _, err := tx.GetWallet(context.Background(), "a"); err != first {
		t.Fatal("read proceeded after SQL failure")
	}
	if _, err := tx.ReserveIdempotencyKey(context.Background(), "k", "hash", "t", time.Time{}); err != first {
		t.Fatal("write proceeded after SQL failure")
	}
}

func TestMissingRowDoesNotPoisonTransaction(t *testing.T) {
	tx := &transaction{}
	if !errors.Is(tx.record("get", sql.ErrNoRows), repository.ErrNotFound) {
		t.Fatal("missing row not recognized")
	}
	if tx.failure != nil {
		t.Fatal("missing row poisoned transaction")
	}
}
