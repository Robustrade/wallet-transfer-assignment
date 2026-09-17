// Package tests provides integration tests for the wallet transfer service.
package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/lib/pq"

	"github.com/shubh/wallet-service/internal/db"
	"github.com/shubh/wallet-service/internal/handler"
	"github.com/shubh/wallet-service/internal/repository"
	"github.com/shubh/wallet-service/internal/service"
)

var (
	testDB     *sql.DB
	testRouter http.Handler
	testSvc    *service.TransferService
)

// TestMain sets up the test database and server once for all tests.
func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required for integration tests")
	}

	var err error
	testDB, err = db.Connect(dbURL)
	if err != nil {
		log.Fatalf("failed to connect to test database: %v", err)
	}
	defer testDB.Close()

	// Run migrations
	if err := db.RunMigrations(testDB, findMigrationFile()); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Wire up layers
	walletRepo := repository.NewWalletRepository(testDB)
	transferRepo := repository.NewTransferRepository(testDB)
	ledgerRepo := repository.NewLedgerRepository(testDB)
	testSvc = service.NewTransferService(
		db.NewSQLTxManager(testDB),
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	transferHandler := handler.NewTransferHandler(testSvc)
	walletHandler := handler.NewWalletHandler(testSvc)
	healthHandler := handler.NewHealthHandler()
	testRouter = handler.NewRouter(transferHandler, walletHandler, healthHandler)

	os.Exit(m.Run())
}

// resetDB cleans all data and re-seeds wallets between tests.
func resetDB(t *testing.T) {
	t.Helper()
	statements := []string{
		"DELETE FROM ledger_entries",
		"DELETE FROM transfers",
		"DELETE FROM wallets",
		`INSERT INTO wallets (id, balance) VALUES
			('wallet_1', 10000),
			('wallet_2', 10000),
			('wallet_3', 5000)
		 ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance`,
	}
	for _, stmt := range statements {
		if _, err := testDB.Exec(stmt); err != nil {
			t.Fatalf("failed to reset DB: %v", err)
		}
	}
}

// findMigrationFile locates the migration file for test runs.
func findMigrationFile() string {
	candidates := []string{
		"internal/db/migrations/001_init.sql",
		"../internal/db/migrations/001_init.sql",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "internal/db/migrations/001_init.sql"
}

// transferRequest is a helper to build transfer request bodies.
type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResponse is the expected JSON response shape.
type transferResponse struct {
	ID             string  `json:"id"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason,omitempty"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

// doTransfer sends a POST /transfers and returns the response.
func doTransfer(t *testing.T, req transferRequest) (*httptest.ResponseRecorder, *transferResponse) {
	t.Helper()
	body, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	testRouter.ServeHTTP(rr, httpReq)

	var resp transferResponse
	// Only try to decode if the status suggests a transfer body exists
	if rr.Code == http.StatusCreated || rr.Code == http.StatusOK || rr.Code == http.StatusUnprocessableEntity {
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
	}
	return rr, &resp
}

// getWallet retrieves a wallet by ID.
func getWallet(t *testing.T, walletID string) *walletResponse {
	t.Helper()
	httpReq := httptest.NewRequest(http.MethodGet, "/wallets/"+walletID, nil)
	rr := httptest.NewRecorder()
	testRouter.ServeHTTP(rr, httpReq)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for wallet %s, got %d: %s", walletID, rr.Code, rr.Body.String())
	}

	var resp walletResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode wallet response: %v", err)
	}
	return &resp
}

// getLedgerCount returns the number of ledger entries for a transfer.
func getLedgerCount(t *testing.T, transferID string) int {
	t.Helper()
	var count int
	err := testDB.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1", transferID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}
	return count
}

// getTotalDebits returns the sum of all DEBIT amounts in the ledger.
func getTotalDebits(t *testing.T) int64 {
	t.Helper()
	var total sql.NullInt64
	err := testDB.QueryRow("SELECT SUM(amount) FROM ledger_entries WHERE entry_type = 'DEBIT'").Scan(&total)
	if err != nil {
		t.Fatalf("failed to sum debits: %v", err)
	}
	if !total.Valid {
		return 0
	}
	return total.Int64
}

// getTotalCredits returns the sum of all CREDIT amounts in the ledger.
func getTotalCredits(t *testing.T) int64 {
	t.Helper()
	var total sql.NullInt64
	err := testDB.QueryRow("SELECT SUM(amount) FROM ledger_entries WHERE entry_type = 'CREDIT'").Scan(&total)
	if err != nil {
		t.Fatalf("failed to sum credits: %v", err)
	}
	if !total.Valid {
		return 0
	}
	return total.Int64
}

// getTransferCount returns the number of transfers with the given idempotency key.
func getTransferCount(t *testing.T, idempotencyKey string) int {
	t.Helper()
	var count int
	err := testDB.QueryRow("SELECT COUNT(*) FROM transfers WHERE idempotency_key = $1", idempotencyKey).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count transfers: %v", err)
	}
	return count
}

// formatWalletBalances returns a formatted string of all wallet balances for debugging.
func formatWalletBalances(t *testing.T) string {
	t.Helper()
	rows, err := testDB.Query("SELECT id, balance FROM wallets ORDER BY id")
	if err != nil {
		t.Fatalf("failed to query wallets: %v", err)
	}
	defer rows.Close()

	result := ""
	for rows.Next() {
		var id string
		var balance int64
		if err := rows.Scan(&id, &balance); err != nil {
			t.Fatalf("failed to scan wallet: %v", err)
		}
		result += fmt.Sprintf("%s=%d ", id, balance)
	}
	return result
}
