//go:build integration

package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
)

func assertStoredBalancedLedger(t *testing.T, ctx context.Context, db *sql.DB, key string) {
	t.Helper()
	var transfer domain.Transfer
	var entries []domain.LedgerEntry
	err := postgres.New(db).WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error {
		var err error
		transfer, err = tx.GetTransferByIdempotencyKey(ctx, key)
		if err != nil {
			return err
		}
		entries, err = tx.ListLedgerEntries(ctx, transfer.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBalancedTransferLedger(t, transfer, entries)
}

func TestPostgresLedgerUniquenessAndReplay(t *testing.T) {
	db, ctx, _ := postgresTestDB(t)
	if _, err := db.ExecContext(ctx, `INSERT INTO wallets(id,balance) VALUES ('a',100),('b',20)`); err != nil {
		t.Fatal(err)
	}
	service := New(postgres.New(db), slog.New(slog.NewTextHandler(io.Discard, nil)))
	original, err := service.Transfer(ctx, request())
	if err != nil {
		t.Fatal(err)
	}
	assertStoredBalancedLedger(t, ctx, db, "key")
	for _, kind := range []domain.LedgerEntryType{domain.LedgerEntryDebit, domain.LedgerEntryCredit} {
		// Fresh IDs isolate the transfer/type uniqueness rule from the primary key.
		// Each Exec is its own implicit transaction; never continue an aborted tx.
		_, err := db.ExecContext(ctx, `INSERT INTO ledger_entries(id,wallet_id,transfer_id,entry_type,amount,created_at) SELECT id || '-duplicate',wallet_id,transfer_id,entry_type,amount,created_at FROM ledger_entries WHERE entry_type=$1`, kind)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "ledger_transfer_type_unique" {
			t.Fatalf("expected transfer/type uniqueness violation for %s: %v", kind, err)
		}
	}
	replay, err := service.Transfer(ctx, request())
	if err != nil || replay != original {
		t.Fatalf("replay changed result: %v", err)
	}
	assertCount(t, ctx, db, "transfers", 1)
	assertCount(t, ctx, db, "ledger_entries", 2)
	assertBalance(t, ctx, db, "a", 70)
	assertBalance(t, ctx, db, "b", 50)
	assertStoredBalancedLedger(t, ctx, db, "key")
}

// Break a wallet FK on the selected insert. This produces a real SQL failure
// after both balances changed, and (for CREDIT) after the DEBIT was inserted.
type failLedgerWork struct {
	repository.UnitOfWork
	kind domain.LedgerEntryType
}

func (w failLedgerWork) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) error {
	return w.UnitOfWork.WithinTransaction(ctx, func(ctx context.Context, tx repository.Tx) error { return fn(ctx, failLedgerTx{tx, w.kind}) })
}

type failLedgerTx struct {
	repository.Tx
	kind domain.LedgerEntryType
}

func (tx failLedgerTx) CreateLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error {
	if entry.Type == tx.kind {
		entry.WalletID = "nonexistent-wallet"
	}
	return tx.Tx.CreateLedgerEntry(ctx, entry)
}
func TestPostgresEitherLedgerFailureRollsBack(t *testing.T) {
	for _, kind := range []domain.LedgerEntryType{domain.LedgerEntryDebit, domain.LedgerEntryCredit} {
		t.Run(string(kind), func(t *testing.T) {
			db, ctx, _ := postgresTestDB(t)
			if _, err := db.ExecContext(ctx, `INSERT INTO wallets(id,balance) VALUES ('a',100),('b',20)`); err != nil {
				t.Fatal(err)
			}
			service := New(failLedgerWork{postgres.New(db), kind}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			result, err := service.Transfer(ctx, request())
			var pgErr *pgconn.PgError
			if !errors.Is(err, repository.ErrTransactionFailed) || !errors.As(err, &pgErr) || pgErr.Code != "23503" || result != (repository.Result{}) {
				t.Fatalf("expected FK failure with no result: %+v %v", result, err)
			}
			assertBalance(t, ctx, db, "a", 100)
			assertBalance(t, ctx, db, "b", 20)
			assertCount(t, ctx, db, "ledger_entries", 0)
			assertCount(t, ctx, db, "transfers", 0)
			assertCount(t, ctx, db, "idempotency_records", 0)
		})
	}
}
