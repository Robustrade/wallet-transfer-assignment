package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"wallet-transfer/internal/database"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"

	_ "github.com/lib/pq"
)

// projectRoot returns the absolute path to the repository root regardless of
// where `go test` is invoked from. runtime.Caller(0) gives the absolute path
// of this source file; we walk up two levels to the project root.
func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// file = .../wallet-transfer/tests/integration/helpers_test.go
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func testDSN() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://postgres:postgres@localhost:5432/wallet_transfer_test?sslmode=disable"
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	adminDB, err := database.Connect("postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	if err != nil {
		t.Fatalf("connecting to admin db: %v", err)
	}
	_, _ = adminDB.Exec(`CREATE DATABASE wallet_transfer_test`)
	adminDB.Close()

	db, err := database.Connect(testDSN())
	if err != nil {
		t.Fatalf("connecting to test db: %v", err)
	}

	migrationsDir := filepath.Join(projectRoot(), "migrations")
	if err := database.RunMigrations(db, migrationsDir); err != nil {
		db.Close()
		t.Fatalf("running migrations: %v", err)
	}

	_, err = db.Exec(`
		TRUNCATE TABLE idempotency_records, ledger_entries, transfers, wallets
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		db.Close()
		t.Fatalf("truncating tables: %v", err)
	}

	t.Cleanup(func() { db.Close() })
	return db
}

func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	transferRepo := repository.NewPostgresTransferRepository(db)
	walletRepo := repository.NewPostgresWalletRepository(db)
	transferSvc := service.NewTransferService(transferRepo, nil)
	transferH := handler.NewTransferHandler(transferSvc)
	walletH := handler.NewWalletHandler(walletRepo)
	router := handler.NewRouter(transferH, walletH)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return srv
}

// --- HTTP helpers ---

type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         any    `json:"amount"`
}

type transferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Balance string `json:"balance"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func createWallet(t *testing.T, srv *httptest.Server, name string, initialBalance int) walletResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "initialBalance": initialBalance})
	resp, err := http.Post(srv.URL+"/wallets", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating wallet: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create wallet got status %d", resp.StatusCode)
	}
	var w walletResponse
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("decoding wallet response: %v", err)
	}
	return w
}

func doTransfer(t *testing.T, srv *httptest.Server, req transferRequest) (int, *transferResponse, *errorResponse) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(srv.URL+"/transfers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("posting transfer: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var tr transferResponse
		if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
			t.Fatalf("decoding transfer response: %v", err)
		}
		return resp.StatusCode, &tr, nil
	}
	var er errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		t.Fatalf("decoding error response: %v", err)
	}
	return resp.StatusCode, nil, &er
}

func getWallet(t *testing.T, srv *httptest.Server, id string) walletResponse {
	t.Helper()
	resp, err := http.Get(srv.URL + "/wallets/" + id)
	if err != nil {
		t.Fatalf("getting wallet: %v", err)
	}
	defer resp.Body.Close()
	var w walletResponse
	if err := json.NewDecoder(resp.Body).Decode(&w); err != nil {
		t.Fatalf("decoding wallet: %v", err)
	}
	return w
}

// --- DB invariant helpers ---

func assertLedgerEntries(t *testing.T, db *sql.DB, transferID string, expectDebit, expectCredit bool) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT entry_type FROM ledger_entries WHERE transfer_id = $1`, transferID)
	if err != nil {
		t.Fatalf("querying ledger entries: %v", err)
	}
	defer rows.Close()
	var debits, credits int
	for rows.Next() {
		var entryType string
		if err := rows.Scan(&entryType); err != nil {
			t.Fatalf("scanning ledger entry: %v", err)
		}
		switch entryType {
		case "DEBIT":
			debits++
		case "CREDIT":
			credits++
		}
	}
	if expectDebit && debits != 1 {
		t.Errorf("expected 1 DEBIT entry, got %d", debits)
	}
	if !expectDebit && debits != 0 {
		t.Errorf("expected 0 DEBIT entries, got %d", debits)
	}
	if expectCredit && credits != 1 {
		t.Errorf("expected 1 CREDIT entry, got %d", credits)
	}
	if !expectCredit && credits != 0 {
		t.Errorf("expected 0 CREDIT entries, got %d", credits)
	}
}

func assertTransferStatus(t *testing.T, db *sql.DB, transferID string, wantStatus domain.TransferStatus) {
	t.Helper()
	var status string
	err := db.QueryRowContext(context.Background(),
		`SELECT status FROM transfers WHERE id = $1`, transferID).Scan(&status)
	if err != nil {
		t.Fatalf("querying transfer status: %v", err)
	}
	if status != string(wantStatus) {
		t.Errorf("transfer status: got %q want %q", status, wantStatus)
	}
}

func assertWalletBalance(t *testing.T, db *sql.DB, walletID string, wantCents int64) {
	t.Helper()
	var balanceStr string
	err := db.QueryRowContext(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balanceStr)
	if err != nil {
		t.Fatalf("querying wallet balance: %v", err)
	}
	got, err := domain.NumericToCents(balanceStr)
	if err != nil {
		t.Fatalf("parsing balance: %v", err)
	}
	if got != wantCents {
		t.Errorf("wallet %s balance: got %d cents want %d cents (%.2f vs %.2f)",
			walletID, got, wantCents, float64(got)/100, float64(wantCents)/100)
	}
}

func totalMoneyInDB(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var sumStr string
	err := db.QueryRowContext(context.Background(),
		`SELECT COALESCE(SUM(balance)::text, '0') FROM wallets`).Scan(&sumStr)
	if err != nil {
		t.Fatalf("summing balances: %v", err)
	}
	sum, err := domain.NumericToCents(sumStr)
	if err != nil {
		t.Fatalf("parsing total balance: %v", err)
	}
	return sum
}

func uniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
