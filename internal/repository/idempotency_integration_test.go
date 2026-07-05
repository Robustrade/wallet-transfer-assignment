//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

func TestIdempotencyRepository_InsertAndGet(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transferRepo := repository.NewTransferRepository()
	transfer, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := transferRepo.InsertTransfer(context.Background(), pool, transfer); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	repo := repository.NewIdempotencyRepository()
	record := domain.IdempotencyRecord{
		IdempotencyKey: "key-1",
		RequestHash:    "hash-1",
		TransferID:     transfer.ID,
		CreatedAt:      time.Now(),
	}
	if err := repo.InsertIdempotencyKey(context.Background(), pool, record); err != nil {
		t.Fatalf("insert idempotency key: %v", err)
	}

	got, err := repo.GetIdempotencyRecord(context.Background(), pool, "key-1")
	if err != nil {
		t.Fatalf("get idempotency record: %v", err)
	}
	if got.TransferID != transfer.ID {
		t.Errorf("expected transfer ID %s, got %s", transfer.ID, got.TransferID)
	}
	if got.RequestHash != "hash-1" {
		t.Errorf("expected request hash %q, got %q", "hash-1", got.RequestHash)
	}
}

func TestIdempotencyRepository_GetIdempotencyRecord_NotFound(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)

	repo := repository.NewIdempotencyRepository()
	_, err := repo.GetIdempotencyRecord(context.Background(), pool, "does-not-exist")
	if !errors.Is(err, domain.ErrIdempotencyRecordNotFound) {
		t.Errorf("expected ErrIdempotencyRecordNotFound, got %v", err)
	}
}

// TestIdempotencyRepository_InsertIdempotencyKey_DuplicateKeyViolatesUnique
// confirms the primary key on idempotency_key is the actual enforcement
// mechanism the service's race handling relies on -- a second insert for an
// existing key must fail with a real Postgres unique_violation (23505), not
// silently succeed or return some other error.
func TestIdempotencyRepository_InsertIdempotencyKey_DuplicateKeyViolatesUnique(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transferRepo := repository.NewTransferRepository()
	transferA, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	transferB, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 75)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := transferRepo.InsertTransfer(context.Background(), pool, transferA); err != nil {
		t.Fatalf("insert transferA: %v", err)
	}
	if err := transferRepo.InsertTransfer(context.Background(), pool, transferB); err != nil {
		t.Fatalf("insert transferB: %v", err)
	}

	repo := repository.NewIdempotencyRepository()
	first := domain.IdempotencyRecord{IdempotencyKey: "dup-key", RequestHash: "hash-a", TransferID: transferA.ID, CreatedAt: time.Now()}
	if err := repo.InsertIdempotencyKey(context.Background(), pool, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	second := domain.IdempotencyRecord{IdempotencyKey: "dup-key", RequestHash: "hash-b", TransferID: transferB.ID, CreatedAt: time.Now()}
	err = repo.InsertIdempotencyKey(context.Background(), pool, second)
	if err == nil {
		t.Fatal("expected unique violation inserting a duplicate idempotency_key, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("expected a unique_violation (23505), got %v", err)
	}
}
