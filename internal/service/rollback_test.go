package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository"
	"github.com/example/wallet-transfer/internal/repository/memory"
	"github.com/example/wallet-transfer/internal/service"
)

// failOnceStore wraps memory.Store and makes the next InsertTransfer call
// fail with an injected error, simulating an unexpected infra failure that
// happens *after* the idempotency key has already been claimed.
type failOnceStore struct {
	inner   repository.TxStore
	trigger bool
}

func (s *failOnceStore) BeginTx(ctx context.Context) (repository.Tx, error) {
	innerTx, err := s.inner.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	return &failOnceTx{Tx: innerTx, store: s}, nil
}

// failOnceTx embeds the real Tx and overrides only InsertTransfer, so every
// other method (including Rollback) delegates straight through.
type failOnceTx struct {
	repository.Tx
	store *failOnceStore
}

var errInjected = errors.New("injected infra failure")

func (t *failOnceTx) InsertTransfer(ctx context.Context, tr domain.Transfer) error {
	if t.store.trigger {
		t.store.trigger = false
		return errInjected
	}
	return t.Tx.InsertTransfer(ctx, tr)
}

// TestCreateTransfer_RollbackReleasesIdempotencyClaimOnUnexpectedFailure
// guards against the bug flagged in review: the in-memory store's Rollback
// used to just release the mutex without undoing any writes, so a request
// that failed unexpectedly after claiming an idempotencyKey left an
// IN_PROGRESS record behind forever — meaning every retry would get
// ErrDuplicateInFlight instead of a fresh attempt.
func TestCreateTransfer_RollbackReleasesIdempotencyClaimOnUnexpectedFailure(t *testing.T) {
	mem := memory.NewStore()
	mem.SeedWallet(domain.Wallet{ID: "wallet_1", Balance: 500})
	mem.SeedWallet(domain.Wallet{ID: "wallet_2", Balance: 100})

	store := &failOnceStore{inner: mem, trigger: true}
	svc := service.NewTransferService(store)

	req := service.CreateTransferRequest{
		IdempotencyKey: "rollback-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50,
	}

	// First attempt claims the idempotency key, then fails partway through
	// executeTransfer for an unrelated (injected) infra reason.
	_, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, errInjected) {
		t.Fatalf("expected the injected error on the first attempt, got %v", err)
	}

	// A retry with the same key must get a genuinely fresh attempt — not
	// ErrDuplicateInFlight, which would mean the claim was never released.
	result, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected retry to succeed after rollback released the claim, got error: %v", err)
	}
	if result.State != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED on retry, got %s", result.State)
	}

	from, _ := mem.GetWallet("wallet_1")
	if from.Balance != 450 {
		t.Fatalf("expected exactly one 50-unit debit to have landed, got balance %d", from.Balance)
	}
}
