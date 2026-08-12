package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/postgres"
)

// testDB connects to TEST_DATABASE_URL and applies migrations. It skips the
// test entirely if the env var is unset, so `go test ./...` stays green on a
// machine with no database — see docs/design.md §2.1.
func testDB(t *testing.T) *postgres.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run integration tests (try: make test)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := postgres.NewDB(ctx, postgres.Config{
		DatabaseURL:      url,
		MaxConns:         5,
		StatementTimeout: 5 * time.Second,
		LockTimeout:      3 * time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, postgres.Migrate(ctx, db.Pool()))
	t.Cleanup(db.Close)
	return db
}

func insertTestWallet(t *testing.T, ctx context.Context, db *postgres.DB, id string, balance int64) {
	t.Helper()
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO wallets (id, kind, currency, balance_minor) VALUES ($1, 'USER', 'INR', $2)`,
		id, balance)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM wallets WHERE id = $1`, id)
	})
}

func insertTestTransfer(t *testing.T, ctx context.Context, db *postgres.DB, id uuid.UUID, from, to string, status string) {
	t.Helper()
	settledAt := "NULL"
	failureCode := "NULL"
	if status == "PROCESSED" {
		settledAt = "now()"
	}
	if status == "FAILED" {
		failureCode = "'INSUFFICIENT_FUNDS'"
	}
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status, settled_at, failure_code)
		VALUES ($1, $2, $3, $4, $5, 100, 'INR', $6, `+settledAt+`, `+failureCode+`)`,
		id, id.String(), "0000000000000000000000000000000000000000000000000000000000000000"[:64], from, to, status)
	require.NoError(t, err)
}

func TestSchema_TransferStatusIsImmutableOnceSettled(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	from := "schema_from_" + uuid.NewString()[:8]
	to := "schema_to_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, from, 1000)
	insertTestWallet(t, ctx, db, to, 0)

	id := uuid.Must(uuid.NewV7())
	insertTestTransfer(t, ctx, db, id, from, to, "PROCESSED")

	_, err := db.Pool().Exec(ctx, `UPDATE transfers SET status = 'FAILED', failure_code = 'INSUFFICIENT_FUNDS' WHERE id = $1`, id)
	require.Error(t, err, "the trigger must reject transitioning a settled transfer")
}

func TestSchema_TransferImmutableColumnsCannotChange(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	from := "schema_from_" + uuid.NewString()[:8]
	to := "schema_to_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, from, 1000)
	insertTestWallet(t, ctx, db, to, 0)

	id := uuid.Must(uuid.NewV7())
	insertTestTransfer(t, ctx, db, id, from, to, "PENDING")

	_, err := db.Pool().Exec(ctx, `UPDATE transfers SET amount_minor = 999 WHERE id = $1`, id)
	require.Error(t, err, "the trigger must reject changing an immutable column")
}

func TestSchema_LedgerEntriesAreAppendOnly(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	from := "schema_from_" + uuid.NewString()[:8]
	to := "schema_to_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, from, 1000)
	insertTestWallet(t, ctx, db, to, 0)

	id := uuid.Must(uuid.NewV7())
	insertTestTransfer(t, ctx, db, id, from, to, "PROCESSED")

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES ($1, $2, 'DEBIT', 100, 'INR', 900)`, id, from)
	require.NoError(t, err)

	_, err = db.Pool().Exec(ctx, `UPDATE ledger_entries SET amount_minor = 1 WHERE transfer_id = $1`, id)
	require.Error(t, err, "ledger_entries must reject UPDATE")

	_, err = db.Pool().Exec(ctx, `DELETE FROM ledger_entries WHERE transfer_id = $1`, id)
	require.Error(t, err, "ledger_entries must reject DELETE")
}

func TestSchema_OnlyOneDebitAndOneCreditPerTransfer(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	from := "schema_from_" + uuid.NewString()[:8]
	to := "schema_to_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, from, 1000)
	insertTestWallet(t, ctx, db, to, 0)

	id := uuid.Must(uuid.NewV7())
	insertTestTransfer(t, ctx, db, id, from, to, "PROCESSED")

	_, err := db.Pool().Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES ($1, $2, 'DEBIT', 100, 'INR', 900)`, id, from)
	require.NoError(t, err)

	_, err = db.Pool().Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES ($1, $2, 'DEBIT', 100, 'INR', 800)`, id, from)
	require.Error(t, err, "a second DEBIT for the same transfer must violate the unique constraint")
}

func TestSchema_UserWalletCannotGoNegative(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	id := "schema_neg_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, id, 100)

	_, err := db.Pool().Exec(ctx, `UPDATE wallets SET balance_minor = -1 WHERE id = $1`, id)
	require.Error(t, err, "a USER wallet must never be allowed a negative balance")
}

func TestSchema_SystemWalletMayGoNegative(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	id := "schema_sys_" + uuid.NewString()[:8]
	_, err := db.Pool().Exec(ctx, `
		INSERT INTO wallets (id, kind, currency, balance_minor) VALUES ($1, 'SYSTEM', 'INR', 0)`, id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM wallets WHERE id = $1`, id)
	})

	_, err = db.Pool().Exec(ctx, `UPDATE wallets SET balance_minor = -500 WHERE id = $1`, id)
	require.NoError(t, err, "a SYSTEM wallet must be allowed to go negative")
}
