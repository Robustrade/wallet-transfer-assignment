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
	"wallet-transfer-assignment/internal/repository/postgres"
)

func TestPostgresOppositeTransfersLockInSameOrder(t *testing.T) {
	db, parentCtx, schema := postgresTestDB(t)
	if _, err := db.ExecContext(parentCtx, `INSERT INTO wallets(id,balance) VALUES ('A',100),('B',100)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(parentCtx)
	// Hold the first ID so both real service transactions stop at their first lock.
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err := blocker.ExecContext(ctx, `SELECT id FROM wallets WHERE id='A' FOR UPDATE`); err != nil {
		cancel()
		t.Fatal(err)
	}
	outcomes := make(chan callOutcome, 2)
	started := make(chan struct{}, 2)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	appName := schema + "_opposite"
	requests := []Request{{"a-to-b", "A", "B", 30}, {"b-to-a", "B", "A", 20}}
	for _, req := range requests {
		cfg, err := pgx.ParseConfig(containerDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		cfg.RuntimeParams["search_path"] = schema
		cfg.RuntimeParams["application_name"] = appName
		pool := stdlib.OpenDB(*cfg)
		pool.SetMaxOpenConns(1)
		t.Cleanup(func() { pool.Close() })
		svc := New(postgres.New(pool), slog.New(slog.NewTextHandler(io.Discard, nil)))
		workers.Add(1)
		go func(req Request, svc *Service) {
			defer workers.Done()
			started <- struct{}{}
			r, e := svc.Transfer(ctx, req)
			outcomes <- callOutcome{r, e}
		}(req, svc)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	// Poll database evidence, not elapsed time, before releasing A. Both calls
	// must be inside FOR UPDATE; a Go mutex cannot satisfy this condition.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
   WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0
   AND query LIKE 'SELECT id, balance%FOR UPDATE'`, appName).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked == 2 {
			break
		}
		select {
		case out := <-outcomes:
			t.Fatalf("transfer returned before locks were released: %+v", out)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	// If B->A locked its source first, B would already be held here. NOWAIT
	// would fail. Its availability proves both directions first wait for A.
	probe, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Rollback()
	if _, err := probe.ExecContext(ctx, `SELECT id FROM wallets WHERE id='B' FOR UPDATE NOWAIT`); err != nil {
		t.Fatalf("B was locked before A; wallet lock ordering is inconsistent: %v", err)
	}
	if err := probe.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case out := <-outcomes:
			if out.err != nil || out.result.StatusCode != 201 {
				t.Fatalf("opposite transfer did not commit: %+v", out)
			}
			var body struct {
				State string `json:"state"`
			}
			if err := json.Unmarshal([]byte(out.result.Body), &body); err != nil || body.State != "PROCESSED" {
				t.Fatalf("unexpected result: %s", out.result.Body)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	workers.Wait()
	// Unequal amounts make a source/destination swap observable in final balances.
	assertBalance(t, ctx, db, "A", 90)
	assertBalance(t, ctx, db, "B", 110)
	assertCount(t, ctx, db, "transfers", 2)
	assertCount(t, ctx, db, "ledger_entries", 4)
	assertCount(t, ctx, db, "idempotency_records", 2)
	for _, req := range requests {
		var from, to, status string
		var amount int64
		if err := db.QueryRowContext(ctx, `SELECT from_wallet_id,to_wallet_id,amount,status FROM transfers WHERE idempotency_key=$1`, req.IdempotencyKey).Scan(&from, &to, &amount, &status); err != nil {
			t.Fatal(err)
		}
		if from != req.FromWalletID || to != req.ToWalletID || amount != req.Amount || status != "PROCESSED" {
			t.Fatalf("business roles changed: %s -> %s %d %s", from, to, amount, status)
		}
		assertStoredBalancedLedger(t, ctx, db, req.IdempotencyKey)
	}
}
