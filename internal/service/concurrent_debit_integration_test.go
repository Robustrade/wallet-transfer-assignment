//go:build integration

package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

// This decorator pauses after the real FOR UPDATE has acquired both rows.
// It delegates every operation to PostgreSQL and adds no mutual exclusion.
type heldWalletWork struct {
	repository.UnitOfWork
	locked  chan struct{}
	release chan struct{}
}

func (w heldWalletWork) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return w.UnitOfWork.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error { return fn(ctx, heldWalletTx{Tx: tx, work: w}) })
}

type heldWalletTx struct {
	repository.Tx
	work heldWalletWork
}

func (tx heldWalletTx) LockWallets(ctx context.Context, fromID, toID string) (domain.Wallet, domain.Wallet, error) {
	from, to, err := tx.Tx.LockWallets(ctx, fromID, toID)
	if err != nil {
		return from, to, err
	}
	close(tx.work.locked)
	select {
	case <-tx.work.release:
		return from, to, nil
	case <-ctx.Done():
		return from, to, ctx.Err()
	}
}

func TestPostgresConcurrentDebitsCannotOverspend(t *testing.T) {
	db, parentCtx, schema := postgresTestDB(t)
	if _, err := db.ExecContext(parentCtx, `INSERT INTO wallets(id,balance) VALUES ('wallet_1',100),('wallet_2',0),('wallet_3',0)`); err != nil {
		t.Fatal(err)
	}

	// Independent connection pools and service instances share only PostgreSQL.
	// Different keys ensure this tests wallet locking, not key arbitration.
	cfg, err := pgx.ParseConfig(containerDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeParams["search_path"] = schema
	contenderApp := schema + "_contender"
	cfg.RuntimeParams["application_name"] = contenderApp
	contenderDB := stdlib.OpenDB(*cfg)
	defer contenderDB.Close()
	ctx, cancel := context.WithCancel(parentCtx)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	held := heldWalletWork{UnitOfWork: postgres.New(db), locked: make(chan struct{}), release: make(chan struct{})}
	winnerDone, loserDone := make(chan callOutcome, 1), make(chan callOutcome, 1)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		select {
		case <-held.release:
		default:
			close(held.release)
		}
		workers.Wait()
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		r, e := New(held, logger).Transfer(ctx, Request{"debit-2", "wallet_1", "wallet_2", 80})
		winnerDone <- callOutcome{r, e}
	}()
	select {
	case <-held.locked:
	case out := <-winnerDone:
		t.Fatalf("first transfer exited before obtaining locks: %v", out.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		r, e := New(postgres.New(contenderDB), logger).Transfer(ctx, Request{"debit-3", "wallet_1", "wallet_3", 80})
		loserDone <- callOutcome{r, e}
	}()

	// Channels control overlap; this ticker only polls observed PostgreSQL state.
	// Require a real blocked FOR UPDATE, rather than assuming overlap from timing.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (
   SELECT 1 FROM pg_stat_activity
   WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0
   AND query LIKE 'SELECT id, balance%FOR UPDATE'
  )`, contenderApp).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case out := <-loserDone:
			t.Fatalf("competing transfer did not wait on the DB row lock: %+v", out)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	// Before release, uncommitted money movement is not visible.
	assertBalance(t, ctx, db, "wallet_1", 100)
	close(held.release)
	var winner, loser callOutcome
	select {
	case winner = <-winnerDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case loser = <-loserDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if winner.err != nil || winner.result.StatusCode != 201 {
		t.Fatalf("winner: %+v", winner)
	}
	if loser.err != nil || loser.result.StatusCode != 422 {
		t.Fatalf("loser: %+v", loser)
	}
	var failed struct {
		State string `json:"state"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(loser.result.Body), &failed); err != nil || failed.State != "FAILED" || failed.Error.Code != "INSUFFICIENT_BALANCE" {
		t.Fatalf("expected insufficient funds after lock wait: %s", loser.result.Body)
	}

	// Assert committed database state directly, including ledger/transfer joins.
	assertBalance(t, ctx, db, "wallet_1", 20)
	assertBalance(t, ctx, db, "wallet_2", 80)
	assertBalance(t, ctx, db, "wallet_3", 0)
	var negative, processed, failedCount, ledgerCount, failedLedger int
	var totalFunds, debitTotal int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE balance<0),sum(balance) FROM wallets`).Scan(&negative, &totalFunds); err != nil {
		t.Fatal(err)
	}
	if negative != 0 || totalFunds != 100 {
		t.Fatalf("wallet invariant: negative=%d total=%d", negative, totalFunds)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='PROCESSED'),count(*) FILTER (WHERE status='FAILED') FROM transfers`).Scan(&processed, &failedCount); err != nil {
		t.Fatal(err)
	}
	if processed != 1 || failedCount != 1 {
		t.Fatalf("transfer states: processed=%d failed=%d", processed, failedCount)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(amount) FILTER (WHERE entry_type='DEBIT' AND wallet_id='wallet_1'),0) FROM ledger_entries`).Scan(&ledgerCount, &debitTotal); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 || debitTotal != 80 {
		t.Fatalf("ledger: rows=%d source debit=%d", ledgerCount, debitTotal)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM ledger_entries l JOIN transfers t ON t.id=l.transfer_id WHERE t.status<>'PROCESSED'`).Scan(&failedLedger); err != nil {
		t.Fatal(err)
	}
	if failedLedger != 0 {
		t.Fatal("ledger entries exist for unsuccessful transfer")
	}
	var status, toID string
	if err := db.QueryRowContext(ctx, `SELECT status,to_wallet_id FROM transfers WHERE idempotency_key='debit-3'`).Scan(&status, &toID); err != nil || status != "FAILED" || toID != "wallet_3" {
		t.Fatalf("wrong failed transfer: %s %s %v", status, toID, err)
	}
	assertStoredBalancedLedger(t, ctx, db, "debit-2")
	assertCount(t, ctx, db, "transfers", 2)
	assertCount(t, ctx, db, "idempotency_records", 2)
}
