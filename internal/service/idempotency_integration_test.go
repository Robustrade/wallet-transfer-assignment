//go:build integration

package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

// Pause the owner immediately after its real INSERT, while its key is uncommitted.
type heldClaimWork struct {
	repository.UnitOfWork
	claimed  chan struct{}
	release  chan struct{}
	rollback bool
}

func (w heldClaimWork) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return w.UnitOfWork.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error { return fn(ctx, heldClaimTx{Tx: tx, work: w}) })
}

type heldClaimTx struct {
	repository.Tx
	work heldClaimWork
}

func (tx heldClaimTx) ReserveIdempotencyKey(ctx context.Context, key, fingerprint, id string, at time.Time) (bool, error) {
	owned, err := tx.Tx.ReserveIdempotencyKey(ctx, key, fingerprint, id, at)
	if err != nil || !owned {
		return owned, err
	}
	close(tx.work.claimed)
	select {
	case <-tx.work.release:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if tx.work.rollback {
		return false, injected
	}
	return true, nil
}

type callOutcome struct {
	result repository.Result
	err    error
}

func TestPostgresConcurrentIdempotency(t *testing.T) {
	for _, scenario := range []string{"same_payload", "different_payload", "failed_original", "rolled_back_original"} {
		t.Run(scenario, func(t *testing.T) {
			db, ctx, appName := postgresTestDB(t)
			if _, err := db.ExecContext(ctx, `INSERT INTO wallets(id,balance) VALUES ('a',100),('b',20)`); err != nil {
				t.Fatal(err)
			}
			work := postgres.New(db)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			held := heldClaimWork{UnitOfWork: work, claimed: make(chan struct{}), release: make(chan struct{}), rollback: scenario == "rolled_back_original"}
			defer func() {
				select {
				case <-held.release:
				default:
					close(held.release)
				}
			}()
			original := request()
			if scenario == "failed_original" {
				original.Amount = 101
			}
			retry := original
			if scenario == "different_payload" {
				retry.Amount++
			}
			ownerDone := make(chan callOutcome, 1)
			retryDone := make(chan callOutcome, 1)
			go func() { r, e := New(held, logger).Transfer(ctx, original); ownerDone <- callOutcome{r, e} }()
			select {
			case <-held.claimed:
			case out := <-ownerDone:
				t.Fatalf("owner failed before claim: %v", out.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			go func() { r, e := New(work, logger).Transfer(ctx, retry); retryDone <- callOutcome{r, e} }()
			// Observe the actual PostgreSQL wait rather than assuming goroutine timing.
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			waiting := false
			for !waiting {
				if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE 'INSERT INTO idempotency_records%')`, appName).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case out := <-retryDone:
					t.Fatalf("retry did not wait for owner: %+v", out)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-ticker.C:
				}
			}
			// Uncommitted reservations and transfers are invisible to other transactions.
			assertCount(t, ctx, db, "idempotency_records", 0)
			assertCount(t, ctx, db, "transfers", 0)
			close(held.release)
			var owner, retried callOutcome
			select {
			case owner = <-ownerDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case retried = <-retryDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch scenario {
			case "rolled_back_original":
				if !errors.Is(owner.err, injected) || retried.err != nil || retried.result.StatusCode != 201 {
					t.Fatalf("failed takeover: %+v %+v", owner, retried)
				}
			case "different_payload":
				if owner.err != nil || !errors.Is(retried.err, ErrIdempotencyConflict) {
					t.Fatalf("expected conflict: %+v %+v", owner, retried)
				}
			default:
				if owner.err != nil || retried.err != nil || owner.result != retried.result {
					t.Fatalf("results differ: %+v %+v", owner, retried)
				}
			}
			assertCount(t, ctx, db, "transfers", 1)
			assertCount(t, ctx, db, "idempotency_records", 1)
			ledgerCount := 2
			from, to := int64(70), int64(50)
			if scenario == "failed_original" {
				ledgerCount = 0
				from, to = 100, 20
				if retried.result.StatusCode != 422 {
					t.Fatal("failed result not replayed")
				}
			}
			assertCount(t, ctx, db, "ledger_entries", ledgerCount)
			if ledgerCount == 2 {
				assertStoredBalancedLedger(t, ctx, db, original.IdempotencyKey)
			}
			assertBalance(t, ctx, db, "a", from)
			assertBalance(t, ctx, db, "b", to)
			// Simulate losing the HTTP result: recover expectations from durable storage,
			// and retry via a new service instance with no cached result.
			var durable repository.Result
			if err := db.QueryRowContext(ctx, `SELECT response_status,response_body FROM idempotency_records WHERE idempotency_key='key'`).Scan(&durable.StatusCode, &durable.Body); err != nil {
				t.Fatal(err)
			}
			replay, err := New(work, logger).Transfer(ctx, original)
			if err != nil || replay != durable {
				t.Fatalf("lost-response replay mismatch: %v", err)
			}
			assertCount(t, ctx, db, "transfers", 1)
			assertCount(t, ctx, db, "ledger_entries", ledgerCount)
			if ledgerCount == 2 {
				assertStoredBalancedLedger(t, ctx, db, original.IdempotencyKey)
			}
			assertBalance(t, ctx, db, "a", from)
			assertBalance(t, ctx, db, "b", to)
		})
	}
}
