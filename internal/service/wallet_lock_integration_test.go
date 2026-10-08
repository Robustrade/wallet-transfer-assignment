//go:build integration

package service

import (
	"context"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

func TestPostgresWalletLockBlocksAndPreservesRoles(t *testing.T) {
	db, ctx, appName := postgresTestDB(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO wallets(id,balance) VALUES ('a',100),('b',20)`); err != nil {
		t.Fatal(err)
	}
	owner, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback()
	if _, err := owner.ExecContext(ctx, `SELECT id FROM wallets WHERE id='a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- postgres.New(db).WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error {
			from, to, err := tx.LockWallets(ctx, "b", "a")
			if err != nil {
				return err
			}
			// Channel carries observations; no fatal test calls inside this goroutine.
			if from.ID != "b" || to.ID != "a" || from.Balance != 20 || to.Balance != 101 {
				return errWrongWalletRoles{}
			}
			return nil
		})
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE 'SELECT id, balance%FOR UPDATE')`, appName).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("wallet lock did not block: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	if _, err := owner.ExecContext(ctx, `UPDATE wallets SET balance=101 WHERE id='a'`); err != nil {
		t.Fatal(err)
	}
	if err := owner.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type errWrongWalletRoles struct{}

func (errWrongWalletRoles) Error() string {
	return "locked wallets have wrong business roles or stale balances"
}
