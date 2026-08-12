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
	require.NoError(t, postgres.Migrate(ctx, db.Gorm()))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertTestWallet(t *testing.T, ctx context.Context, db *postgres.DB, id string, balance int64) {
	t.Helper()
	err := db.Gorm().WithContext(ctx).Exec(
		`INSERT INTO wallets (id, kind, currency, balance_minor) VALUES (?, 'USER', 'INR', ?)`,
		id, balance).Error
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Gorm().WithContext(context.Background()).Exec(`DELETE FROM wallets WHERE id = ?`, id)
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
	err := db.Gorm().WithContext(ctx).Exec(`
		INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, amount_minor, currency, status, settled_at, failure_code)
		VALUES (?, ?, ?, ?, ?, 100, 'INR', ?, `+settledAt+`, `+failureCode+`)`,
		id, id.String(), "0000000000000000000000000000000000000000000000000000000000000000"[:64], from, to, status).Error
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

	err := db.Gorm().WithContext(ctx).Exec(`UPDATE transfers SET status = 'FAILED', failure_code = 'INSUFFICIENT_FUNDS' WHERE id = ?`, id).Error
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

	err := db.Gorm().WithContext(ctx).Exec(`UPDATE transfers SET amount_minor = 999 WHERE id = ?`, id).Error
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

	err := db.Gorm().WithContext(ctx).Exec(`
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES (?, ?, 'DEBIT', 100, 'INR', 900)`, id, from).Error
	require.NoError(t, err)

	err = db.Gorm().WithContext(ctx).Exec(`UPDATE ledger_entries SET amount_minor = 1 WHERE transfer_id = ?`, id).Error
	require.Error(t, err, "ledger_entries must reject UPDATE")

	err = db.Gorm().WithContext(ctx).Exec(`DELETE FROM ledger_entries WHERE transfer_id = ?`, id).Error
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

	err := db.Gorm().WithContext(ctx).Exec(`
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES (?, ?, 'DEBIT', 100, 'INR', 900)`, id, from).Error
	require.NoError(t, err)

	err = db.Gorm().WithContext(ctx).Exec(`
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor, currency, balance_after)
		VALUES (?, ?, 'DEBIT', 100, 'INR', 800)`, id, from).Error
	require.Error(t, err, "a second DEBIT for the same transfer must violate the unique constraint")
}

func TestSchema_UserWalletCannotGoNegative(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	id := "schema_neg_" + uuid.NewString()[:8]
	insertTestWallet(t, ctx, db, id, 100)

	err := db.Gorm().WithContext(ctx).Exec(`UPDATE wallets SET balance_minor = -1 WHERE id = ?`, id).Error
	require.Error(t, err, "a USER wallet must never be allowed a negative balance")
}

func TestSchema_SystemWalletMayGoNegative(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	id := "schema_sys_" + uuid.NewString()[:8]
	err := db.Gorm().WithContext(ctx).Exec(
		`INSERT INTO wallets (id, kind, currency, balance_minor) VALUES (?, 'SYSTEM', 'INR', 0)`, id).Error
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Gorm().WithContext(context.Background()).Exec(`DELETE FROM wallets WHERE id = ?`, id)
	})

	err = db.Gorm().WithContext(ctx).Exec(`UPDATE wallets SET balance_minor = -500 WHERE id = ?`, id).Error
	require.NoError(t, err, "a SYSTEM wallet must be allowed to go negative")
}
