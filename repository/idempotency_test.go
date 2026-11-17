package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
)

func TestIdempotencyRepository_ClaimReplayMismatch(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	ctx := context.Background()
	repo := repository.NewIdempotencyRepository(pool)

	hash, body, _ := model.HashTransferRequest("a", "b", 10)
	rec, _ := model.NewIdempotencyRecord("idem-1", hash, body)

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(txCtx, rec)
	if err != nil || !claimed {
		t.Fatalf("first claim: %v %v", claimed, err)
	}
	if err := repo.StoreResponse(txCtx, "idem-1", 201, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	txCtx2, tx2, _ := repository.BeginTx(ctx, pool)
	defer func() { _ = tx2.Rollback(ctx) }()
	claimed, err = repo.Claim(txCtx2, rec)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("second claim must not own the key")
	}
	stored, err := repo.Get(txCtx2, "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.RequestHash != hash {
		t.Fatal("hash mismatch unexpectedly")
	}
	if !stored.HasResponse() || *stored.ResponseCode != 201 {
		t.Fatalf("%+v", stored)
	}
	_ = tx2.Rollback(ctx)

	otherHash, otherBody, _ := model.HashTransferRequest("a", "b", 99)
	if otherHash == hash {
		t.Fatal("hashes should differ")
	}
	_ = otherBody
	// mismatch is a service concern: compare hashes after failed claim
	if stored.RequestHash == otherHash {
		t.Fatal("different body must produce different hash")
	}
}

func TestIdempotency_UnknownWalletRollbackReleasesKey(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	ctx := context.Background()
	repo := repository.NewIdempotencyRepository(pool)

	hash, body, _ := model.HashTransferRequest("missing", "also", 10)
	rec, _ := model.NewIdempotencyRecord("temp-key", hash, body)

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(txCtx, rec)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	// simulate unknown wallet → rollback
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Get(ctx, "temp-key"); err != model.ErrNotFound {
		t.Fatalf("key should be gone after rollback: %v", err)
	}

	// retry can claim again
	txCtx2, tx2, _ := repository.BeginTx(ctx, pool)
	defer func() { _ = tx2.Rollback(ctx) }()
	claimed, err = repo.Claim(txCtx2, rec)
	if err != nil || !claimed {
		t.Fatalf("reclaim: %v %v", claimed, err)
	}
}

func TestIdempotencyRepository_StoreResponseRejectsOverwrite(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	ctx := context.Background()
	repo := repository.NewIdempotencyRepository(pool)

	hash, body, _ := model.HashTransferRequest("a", "b", 10)
	rec, _ := model.NewIdempotencyRecord("idem-once", hash, body)

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.Claim(txCtx, rec)
	if err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}
	original := []byte(`{"status":"FAILED","failureReason":"insufficient_funds"}`)
	if err := repo.StoreResponse(txCtx, "idem-once", 422, original); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	txCtx2, tx2, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()
	err = repo.StoreResponse(txCtx2, "idem-once", 201, []byte(`{"status":"PROCESSED"}`))
	if !errors.Is(err, model.ErrConflict) {
		t.Fatalf("second store: %v, want ErrConflict", err)
	}
	_ = tx2.Rollback(ctx)

	stored, err := repo.Get(ctx, "idem-once")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.HasResponse() || *stored.ResponseCode != 422 {
		t.Fatalf("stored response overwritten: %+v", stored)
	}
	var got, want map[string]any
	if err := json.Unmarshal(stored.ResponseBody, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(original, &want); err != nil {
		t.Fatal(err)
	}
	if got["status"] != want["status"] || got["failureReason"] != want["failureReason"] {
		t.Fatalf("response body=%s, want semantic equal of %s", stored.ResponseBody, original)
	}
}
