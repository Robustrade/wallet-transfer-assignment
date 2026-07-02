package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
)

const (
	WalletAlice = "wallet-alice"
	WalletBob   = "wallet-bob"

	AliceSeedBalance int64 = 100000
	BobSeedBalance   int64 = 50000
)

// Shared across all test packages so handler and service tests don't hit the db at the same time.
const testAdvisoryLockKey int64 = 42424242

func DatabaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	return "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"
}

// Setup connects to postgres, locks so integration tests do not run in parallel, and resets data.
func Setup(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), DatabaseURL())
	if err != nil {
		t.Fatalf("database connect: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("database ping: %v", err)
	}
	t.Cleanup(pool.Close)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `SELECT pg_advisory_lock($1)`, testAdvisoryLockKey); err != nil {
		t.Fatalf("advisory lock: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, testAdvisoryLockKey)
	})

	ResetDB(t, pool)
	return pool
}

func ResetDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE idempotency_records, ledger_entries, transfers RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("reset db truncate: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`,
		AliceSeedBalance, WalletAlice,
	); err != nil {
		t.Fatalf("reset alice balance: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`,
		BobSeedBalance, WalletBob,
	); err != nil {
		t.Fatalf("reset bob balance: %v", err)
	}
}

func SetWalletBalance(t *testing.T, pool *pgxpool.Pool, walletID string, balance int64) {
	t.Helper()

	_, err := pool.Exec(context.Background(),
		`UPDATE wallets SET balance = $2, updated_at = NOW() WHERE id = $1`,
		walletID, balance,
	)
	if err != nil {
		t.Fatalf("set wallet balance: %v", err)
	}
}

func WalletBalance(t *testing.T, pool *pgxpool.Pool, walletID string) int64 {
	t.Helper()

	var balance int64
	err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID,
	).Scan(&balance)
	if err != nil {
		t.Fatalf("get wallet balance: %v", err)
	}
	return balance
}

func TransferCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM transfers`).Scan(&count)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	return count
}

func TransferStatus(t *testing.T, pool *pgxpool.Pool, transferID string) domain.TransferStatus {
	t.Helper()

	var status domain.TransferStatus
	err := pool.QueryRow(context.Background(),
		`SELECT status FROM transfers WHERE id = $1`, transferID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("get transfer status: %v", err)
	}
	return status
}

func LedgerEntryCount(t *testing.T, pool *pgxpool.Pool, transferID string) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transferID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}

func TotalLedgerEntries(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_entries`).Scan(&count)
	if err != nil {
		t.Fatalf("count all ledger entries: %v", err)
	}
	return count
}

func HasIdempotencyRecord(t *testing.T, pool *pgxpool.Pool, key string) bool {
	t.Helper()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM idempotency_records WHERE idempotency_key = $1)`, key,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check idempotency record: %v", err)
	}
	return exists
}

func LedgerEntryTypes(t *testing.T, pool *pgxpool.Pool, transferID string) (debitWallet, creditWallet string, amount int64) {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT wallet_id, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY entry_type
	`, transferID)
	if err != nil {
		t.Fatalf("query ledger entries: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var walletID, entryType string
		var entryAmount int64
		if err := rows.Scan(&walletID, &entryType, &entryAmount); err != nil {
			t.Fatalf("scan ledger entry: %v", err)
		}
		switch entryType {
		case string(domain.DEBIT):
			debitWallet = walletID
			amount = entryAmount
		case string(domain.CREDIT):
			creditWallet = walletID
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("ledger rows: %v", err)
	}
	return debitWallet, creditWallet, amount
}

func NewIdempotencyKey(t *testing.T) string {
	t.Helper()
	return uuid.NewString()
}
