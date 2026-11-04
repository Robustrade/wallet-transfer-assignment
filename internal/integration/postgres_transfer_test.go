package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/handler"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/service"
	"github.com/abhiabhi0/wallet-transfer-assignment/migrations"
)

type transferAPIResponse struct {
	Transfer *struct {
		ID          string                `json:"id"`
		Status      domain.TransferStatus `json:"status"`
		FailureCode string                `json:"failureCode"`
	} `json:"transfer"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type transferInput struct {
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

func TestPostgresTransferHTTPAndIdempotency(t *testing.T) {
	db := newPostgresTestDB(t)
	insertWallet(t, db, "replay-source", 1000)
	insertWallet(t, db, "replay-destination", 0)

	api := newTransferAPI(db)
	first := sendTransfer(api, "replay-key", transferInput{"replay-source", "replay-destination", 125})
	if first.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, body = %s", first.Code, first.Body.String())
	}
	firstBody := decodeResponse(t, first)
	if firstBody.Transfer == nil || firstBody.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("first response = %+v", firstBody)
	}

	replay := sendTransfer(api, "replay-key", transferInput{"replay-source", "replay-destination", 125})
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay status = %d, body = %s", replay.Code, replay.Body.String())
	}
	replayBody := decodeResponse(t, replay)
	if replayBody.Transfer == nil || replayBody.Transfer.ID != firstBody.Transfer.ID {
		t.Fatalf("replay did not return original transfer: first=%+v replay=%+v", firstBody, replayBody)
	}

	conflict := sendTransfer(api, "replay-key", transferInput{"replay-source", "replay-destination", 126})
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflicting payload status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	if got := walletBalance(t, db, "replay-source"); got != 875 {
		t.Fatalf("source balance = %d, want 875", got)
	}
	if got := countRows(t, db, "transfers"); got != 1 {
		t.Fatalf("transfer count = %d, want 1", got)
	}
	if got := countLedgerEntries(t, db, firstBody.Transfer.ID); got != 2 {
		t.Fatalf("ledger entry count = %d, want 2", got)
	}
}

func TestPostgresConcurrentSameIdempotencyKeyExecutesOnce(t *testing.T) {
	db := newPostgresTestDB(t)
	insertWallet(t, db, "concurrent-source", 1000)
	insertWallet(t, db, "concurrent-destination", 0)
	api := newTransferAPI(db)

	const requestCount = 12
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, requestCount)
	var wait sync.WaitGroup
	for i := 0; i < requestCount; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- sendTransfer(api, "concurrent-same-key", transferInput{"concurrent-source", "concurrent-destination", 100})
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	var transferID string
	for response := range results {
		if response.Code != http.StatusCreated {
			t.Fatalf("concurrent same-key status = %d, body = %s", response.Code, response.Body.String())
		}
		body := decodeResponse(t, response)
		if body.Transfer == nil {
			t.Fatalf("missing transfer in response: %s", response.Body.String())
		}
		if transferID == "" {
			transferID = body.Transfer.ID
		} else if body.Transfer.ID != transferID {
			t.Fatalf("concurrent requests returned different transfer IDs: %q and %q", transferID, body.Transfer.ID)
		}
	}

	if got := walletBalance(t, db, "concurrent-source"); got != 900 {
		t.Fatalf("source balance = %d, want 900", got)
	}
	if got := walletBalance(t, db, "concurrent-destination"); got != 100 {
		t.Fatalf("destination balance = %d, want 100", got)
	}
	if got := countRows(t, db, "transfers"); got != 1 {
		t.Fatalf("transfer count = %d, want 1", got)
	}
	if got := countLedgerEntries(t, db, transferID); got != 2 {
		t.Fatalf("ledger entry count = %d, want 2", got)
	}
}

func TestPostgresConcurrentDebitsCannotOverdrawWallet(t *testing.T) {
	db := newPostgresTestDB(t)
	insertWallet(t, db, "race-source", 100)
	insertWallet(t, db, "race-destination-a", 0)
	insertWallet(t, db, "race-destination-b", 0)
	api := newTransferAPI(db)

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var wait sync.WaitGroup
	for _, request := range []struct {
		key string
		to  string
	}{
		{key: "race-key-a", to: "race-destination-a"},
		{key: "race-key-b", to: "race-destination-b"},
	} {
		wait.Add(1)
		go func(key, to string) {
			defer wait.Done()
			<-start
			results <- sendTransfer(api, key, transferInput{"race-source", to, 80})
		}(request.key, request.to)
	}
	close(start)
	wait.Wait()
	close(results)

	created, rejected := 0, 0
	for response := range results {
		switch response.Code {
		case http.StatusCreated:
			created++
		case http.StatusUnprocessableEntity:
			rejected++
			body := decodeResponse(t, response)
			if body.Transfer == nil || body.Transfer.Status != domain.TransferFailed ||
				body.Transfer.FailureCode != "INSUFFICIENT_FUNDS" {
				t.Fatalf("unexpected rejected transfer response: %+v", body)
			}
		default:
			t.Fatalf("unexpected concurrent debit status = %d, body = %s", response.Code, response.Body.String())
		}
	}
	if created != 1 || rejected != 1 {
		t.Fatalf("created=%d rejected=%d, want one of each", created, rejected)
	}
	if got := walletBalance(t, db, "race-source"); got != 20 {
		t.Fatalf("source balance = %d, want 20", got)
	}
	if got := walletBalance(t, db, "race-destination-a") + walletBalance(t, db, "race-destination-b"); got != 80 {
		t.Fatalf("destination balances total = %d, want 80", got)
	}
	if got := countRows(t, db, "transfers"); got != 2 {
		t.Fatalf("transfer count = %d, want 2 (one processed, one failed)", got)
	}
	if got := countAllLedgerEntries(t, db); got != 2 {
		t.Fatalf("ledger entry count = %d, want 2", got)
	}
}

func newPostgresTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL admin connection: %v", err)
	}
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		_ = admin.Close()
		t.Fatalf("generate test schema: %v", err)
	}
	schema := "wallet_test_" + hex.EncodeToString(suffix[:])
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		_ = admin.Close()
		t.Fatalf("create test schema: %v", err)
	}

	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	db, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated PostgreSQL connection: %v", err)
	}
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	// Applying twice proves the migration runner is idempotent.
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}
	return db
}

func newTransferAPI(db *sql.DB) http.Handler {
	transferService := service.NewTransferService(service.SQLTransactionManager{DB: db})
	return handler.NewTransferHandler(transferService)
}

func insertWallet(t *testing.T, db *sql.DB, id string, balance int64) {
	t.Helper()
	if _, err := db.Exec(
		"INSERT INTO wallets (id, currency, balance) VALUES ($1, 'INR', $2)", id, balance,
	); err != nil {
		t.Fatalf("insert wallet %q: %v", id, err)
	}
}

func sendTransfer(api http.Handler, key string, input transferInput) *httptest.ResponseRecorder {
	body, _ := json.Marshal(input)
	request := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder) transferAPIResponse {
	t.Helper()
	var body transferAPIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode API response: %v; body=%s", err, response.Body.String())
	}
	return body
}

func walletBalance(t *testing.T, db *sql.DB, walletID string) int64 {
	t.Helper()
	var balance int64
	if err := db.QueryRow("SELECT balance FROM wallets WHERE id = $1", walletID).Scan(&balance); err != nil {
		t.Fatalf("read wallet balance %q: %v", walletID, err)
	}
	return balance
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}

func countLedgerEntries(t *testing.T, db *sql.DB, transferID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1", transferID).Scan(&count); err != nil {
		t.Fatalf("count ledger entries for %s: %v", transferID, err)
	}
	return count
}

func countAllLedgerEntries(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM ledger_entries").Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}

var _ = fmt.Sprintf
