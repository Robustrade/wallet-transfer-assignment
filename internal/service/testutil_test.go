package service_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/db"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

func newTestService(t *testing.T) (*service.TransferService, *sql.DB) {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping service integration tests")
	}

	conn, err := db.Connect(dbURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	svc := service.NewTransferService(conn, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	return svc, conn
}

// resetTables truncates all transactional tables between tests, keeping
// wallet rows (re-seeded with fixed balances) so tests are independent.
func resetTables(t *testing.T, conn *sql.DB) {
	t.Helper()

	_, err := conn.Exec(`TRUNCATE idempotency_records, ledger_entries, transfers RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}

	_, err = conn.Exec(`
		INSERT INTO wallets (id, balance)
		VALUES
			('11111111-1111-1111-1111-111111111111', 100000),
			('22222222-2222-2222-2222-222222222222', 50000)
		ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance
	`)
	if err != nil {
		t.Fatalf("reset wallets: %v", err)
	}
}

const (
	walletA = "11111111-1111-1111-1111-111111111111" // seeded balance: 100000
	walletB = "22222222-2222-2222-2222-222222222222" // seeded balance: 50000
)
