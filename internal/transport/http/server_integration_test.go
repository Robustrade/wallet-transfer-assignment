package transporthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/application"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func connectTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	connString := os.Getenv("PGX_TEST_CONN_STRING")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("integration test requires PGX_TEST_CONN_STRING or DATABASE_URL")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}

	schemaName := fmt.Sprintf("test_http_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{schemaName}.Sanitize())); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to parse conn string: %v", err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		adminPool.Close()
		t.Fatalf("failed to connect with schema search path: %v", err)
	}

	if err := createSchemaObjects(ctx, pool); err != nil {
		pool.Close()
		adminPool.Close()
		t.Fatalf("failed to create schema objects: %v", err)
	}

	cleanup := func() {
		pool.Close()
		if _, err := adminPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA %s CASCADE", pgx.Identifier{schemaName}.Sanitize())); err != nil {
			t.Logf("failed to drop schema %s: %v", schemaName, err)
		}
		adminPool.Close()
	}

	return pool, cleanup
}

func createSchemaObjects(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, strings.TrimSpace(`
CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,
    amount BIGINT NOT NULL,
    status transfer_status NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id UUID REFERENCES transfers(id),
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    response_json JSONB NOT NULL DEFAULT '{}',
    status_code INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_created_at ON ledger_entries(created_at DESC);
CREATE INDEX idx_ledger_wallet_type ON ledger_entries(wallet_id, entry_type) INCLUDE (amount);
CREATE INDEX idx_idempotency_transfer ON idempotency_records(transfer_id);
CREATE INDEX idx_idempotency_created_at ON idempotency_records(created_at DESC);
CREATE INDEX idx_idempotency_pending ON idempotency_records(created_at) WHERE status = 'PENDING';
`))
	return err
}

func TestServer_EndToEndTransferWorkflow(t *testing.T) {
	pool, cleanup := connectTestDB(t)
	defer cleanup()

	walletRepo := repository.NewPostgresWalletRepository(pool)
	transferRepo := repository.NewPostgresTransferRepository(pool)
	ledgerRepo := repository.NewPostgresLedgerRepository(pool)
	idemRepo := repository.NewPostgresIdempotencyRepository(pool)
	txManager := repository.NewPostgresTransactionManager(pool)

	walletService := application.NewWalletService(walletRepo, ledgerRepo)
	transferService := application.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo, idemRepo, nil)
	server := NewServer(walletService, transferService, application.NewHealthServiceFromPgxPool(pool))

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	createWallet(t, mux, "wallet-a", 1000)
	createWallet(t, mux, "wallet-b", 0)

	transferReq := createTransferRequest{
		TransferID:     "11111111-1111-1111-1111-111111111111",
		IdempotencyKey: "idem-end-to-end-1",
		FromWalletID:   "wallet-a",
		ToWalletID:     "wallet-b",
		Amount:         250,
		RequestHash:    "hash-end-to-end-1",
	}

	transferResp := postJSON[transferResponse](t, mux, "/transfers", transferReq, http.StatusCreated)
	if transferResp.TransferID != transferReq.TransferID {
		t.Fatalf("expected transfer id %s, got %s", transferReq.TransferID, transferResp.TransferID)
	}
	if transferResp.Status != "PROCESSED" {
		t.Fatalf("expected transfer status PROCESSED, got %s", transferResp.Status)
	}

	// Repeat the same request to exercise idempotency replay.
	retryResp := postJSON[transferResponse](t, mux, "/transfers", transferReq, http.StatusCreated)
	if retryResp.TransferID != transferResp.TransferID {
		t.Fatalf("expected repeated idempotent call to return same transfer id, got %s", retryResp.TransferID)
	}

	getTransferResp := getJSON[transferResponse](t, mux, "/transfers/"+transferReq.TransferID, http.StatusOK)
	if getTransferResp.TransferID != transferReq.TransferID {
		t.Fatalf("expected get transfer id %s, got %s", transferReq.TransferID, getTransferResp.TransferID)
	}

	walletAResp := getJSON[walletResponse](t, mux, "/wallets/wallet-a", http.StatusOK)
	walletBResp := getJSON[walletResponse](t, mux, "/wallets/wallet-b", http.StatusOK)
	if walletAResp.Balance != 750 {
		t.Fatalf("expected wallet-a balance 750, got %d", walletAResp.Balance)
	}
	if walletBResp.Balance != 250 {
		t.Fatalf("expected wallet-b balance 250, got %d", walletBResp.Balance)
	}

	reconcileA := getJSON[walletReconciliationResponse](t, mux, "/wallets/wallet-a/reconcile", http.StatusOK)
	if !reconcileA.Match || reconcileA.StoredBalance != 750 || reconcileA.LedgerBalance != 750 {
		t.Fatalf("expected wallet-a reconcile to match with 750, got stored=%d ledger=%d match=%v", reconcileA.StoredBalance, reconcileA.LedgerBalance, reconcileA.Match)
	}
}

func postJSON[T any](t *testing.T, mux *http.ServeMux, url string, payload any, expectedStatus int) T {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != expectedStatus {
		t.Fatalf("unexpected status code for POST %s: got %d, want %d, body=%s", url, res.Code, expectedStatus, strings.TrimSpace(res.Body.String()))
	}

	var response T
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode POST %s response: %v", url, err)
	}
	return response
}

func getJSON[T any](t *testing.T, mux *http.ServeMux, url string, expectedStatus int) T {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != expectedStatus {
		t.Fatalf("unexpected status code for GET %s: got %d, want %d, body=%s", url, res.Code, expectedStatus, strings.TrimSpace(res.Body.String()))
	}

	var response T
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode GET %s response: %v", url, err)
	}
	return response
}

func createWallet(t *testing.T, mux *http.ServeMux, walletID string, balance int64) {
	t.Helper()
	request := createWalletRequest{ID: walletID, Balance: balance}
	resp := postJSON[walletResponse](t, mux, "/wallets", request, http.StatusCreated)
	if resp.ID != walletID {
		t.Fatalf("expected created wallet id %s, got %s", walletID, resp.ID)
	}
}
