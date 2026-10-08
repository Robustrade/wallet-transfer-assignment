//go:build integration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

// The barrier releases all callers immediately before their actual PostgreSQL
// INSERT. Every caller already has an independent active database transaction.
// It controls test scheduling only; the DB decides which caller owns the key.
type claimBarrierWork struct {
	repository.UnitOfWork
	ready chan<- struct{}
	start <-chan struct{}
}

func (w claimBarrierWork) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return w.UnitOfWork.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error {
		return fn(ctx, claimBarrierTx{Tx: tx, ready: w.ready, start: w.start})
	})
}

type claimBarrierTx struct {
	repository.Tx
	ready chan<- struct{}
	start <-chan struct{}
}

func (tx claimBarrierTx) ReserveIdempotencyKey(ctx context.Context, key, fingerprint, id string, at time.Time) (bool, error) {
	select {
	case tx.ready <- struct{}{}:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case <-tx.start:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return tx.Tx.ReserveIdempotencyKey(ctx, key, fingerprint, id, at)
}

func TestPostgresSimultaneousSameKeyRequests(t *testing.T) {
	db, parentCtx, schema := postgresTestDB(t)
	if _, err := db.ExecContext(parentCtx, `INSERT INTO wallets(id,balance) VALUES ('wallet_1',100),('wallet_2',0)`); err != nil {
		t.Fatal(err)
	}
	const callers = 8
	ctx, cancel := context.WithCancel(parentCtx)
	ready := make(chan struct{}, callers)
	start := make(chan struct{})
	outcomes := make(chan callOutcome, callers)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		select {
		case <-start:
		default:
			close(start)
		}
		workers.Wait()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	req := Request{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	for i := 0; i < callers; i++ {
		cfg, err := pgx.ParseConfig(containerDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		cfg.RuntimeParams["search_path"] = schema
		cfg.RuntimeParams["application_name"] = fmt.Sprintf("%s_caller_%d", schema, i)
		pool := stdlib.OpenDB(*cfg)
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { pool.Close() })
		svc := New(claimBarrierWork{UnitOfWork: postgres.New(pool), ready: ready, start: start}, logger)
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := svc.Transfer(ctx, req)
			outcomes <- callOutcome{result, err}
		}()
	}
	for i := 0; i < callers; i++ {
		select {
		case <-ready:
		case out := <-outcomes:
			t.Fatalf("caller exited before all transactions reached the claim: %v", out.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(start)
	var original repository.Result
	for i := 0; i < callers; i++ {
		select {
		case out := <-outcomes:
			if out.err != nil || out.result.StatusCode != 201 {
				t.Fatalf("caller %d failed: %+v", i, out)
			}
			if i == 0 {
				original = out.result
			} else if out.result != original {
				t.Fatalf("caller %d received a different original result: %+v versus %+v", i, out.result, original)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	workers.Wait()
	var body struct {
		TransferID   string `json:"transferId"`
		State        string `json:"state"`
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}
	if err := json.Unmarshal([]byte(original.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.TransferID == "" || body.State != "PROCESSED" || body.FromWalletID != req.FromWalletID || body.ToWalletID != req.ToWalletID || body.Amount != 100 {
		t.Fatalf("unexpected result: %+v", body)
	}

	// Direct SQL assertions establish one durable logical transfer and key record.
	assertCount(t, ctx, db, "transfers", 1)
	assertCount(t, ctx, db, "idempotency_records", 1)
	assertCount(t, ctx, db, "ledger_entries", 2)
	assertBalance(t, ctx, db, "wallet_1", 0)
	assertBalance(t, ctx, db, "wallet_2", 100)
	var transferID, key, status string
	if err := db.QueryRowContext(ctx, `SELECT id,idempotency_key,status FROM transfers`).Scan(&transferID, &key, &status); err != nil {
		t.Fatal(err)
	}
	if transferID != body.TransferID || key != "abc123" || status != "PROCESSED" {
		t.Fatalf("unexpected stored transfer: %s %s %s", transferID, key, status)
	}
	var storedID string
	var durable repository.Result
	if err := db.QueryRowContext(ctx, `SELECT transfer_id,response_status,response_body FROM idempotency_records WHERE idempotency_key='abc123'`).Scan(&storedID, &durable.StatusCode, &durable.Body); err != nil {
		t.Fatal(err)
	}
	if storedID != body.TransferID || durable != original {
		t.Fatal("callers did not receive the durable original result")
	}
	var debits, credits int
	var debitTotal, creditTotal int64
	if err := db.QueryRowContext(ctx, `SELECT
  count(*) FILTER (WHERE entry_type='DEBIT' AND wallet_id='wallet_1'),
  count(*) FILTER (WHERE entry_type='CREDIT' AND wallet_id='wallet_2'),
  COALESCE(sum(amount) FILTER (WHERE entry_type='DEBIT'),0),
  COALESCE(sum(amount) FILTER (WHERE entry_type='CREDIT'),0)
  FROM ledger_entries WHERE transfer_id=$1`, body.TransferID).Scan(&debits, &credits, &debitTotal, &creditTotal); err != nil {
		t.Fatal(err)
	}
	if debits != 1 || credits != 1 || debitTotal != 100 || creditTotal != 100 {
		t.Fatalf("duplicate/incorrect ledger: debits=%d credits=%d debit=%d credit=%d", debits, credits, debitTotal, creditTotal)
	}
	assertStoredBalancedLedger(t, ctx, db, "abc123")
}
