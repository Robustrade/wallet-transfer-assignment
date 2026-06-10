//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
)

// ── Response types for JSON unmarshalling ─────────────────────────────────────

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *apiError       `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type transferData struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	ErrorReason    string `json:"errorReason,omitempty"`
}

type walletData struct {
	ID       string `json:"id"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// ── Transfer Execution Tests ──────────────────────────────────────────────────

func TestCreateTransfer_Success(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	transfer := doTransfer(t, "tx-success-1", "wallet_a", "wallet_b", 200, http.StatusCreated)

	assertEqual(t, "PROCESSED", transfer.Status, "transfer status")
	assertEqual(t, int64(200), transfer.Amount, "transfer amount")
	assertEqual(t, "wallet_a", transfer.FromWalletID, "from wallet")
	assertEqual(t, "wallet_b", transfer.ToWalletID, "to wallet")

	// Verify balances were updated correctly
	walletA := getWallet(t, "wallet_a")
	walletB := getWallet(t, "wallet_b")
	assertEqual(t, int64(800), walletA.Balance, "wallet_a balance")
	assertEqual(t, int64(700), walletB.Balance, "wallet_b balance")
}

func TestCreateTransfer_LedgerCorrectness(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 0)

	transfer := doTransfer(t, "tx-ledger-1", "wallet_a", "wallet_b", 300, http.StatusCreated)

	// Verify exactly 2 ledger entries exist for this transfer
	var count int
	err := testDB.Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1::uuid", transfer.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count ledger entries: %v", err)
	}
	assertEqual(t, 2, count, "ledger entry count")

	// Verify debit and credit amounts
	var debitAmount, creditAmount int64
	rows, err := testDB.Pool.Query(context.Background(),
		"SELECT entry_type, amount FROM ledger_entries WHERE transfer_id = $1::uuid ORDER BY entry_type",
		transfer.ID,
	)
	if err != nil {
		t.Fatalf("failed to query ledger entries: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var entryType string
		var amount int64
		if err := rows.Scan(&entryType, &amount); err != nil {
			t.Fatalf("failed to scan ledger entry: %v", err)
		}
		switch entryType {
		case "CREDIT":
			creditAmount = amount
		case "DEBIT":
			debitAmount = amount
		}
	}
	assertEqual(t, int64(300), debitAmount, "debit amount")
	assertEqual(t, int64(300), creditAmount, "credit amount")
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 50)
	createTestWallet(t, "wallet_b", 100)

	body := `{"idempotencyKey":"tx-insuf-1","fromWalletId":"wallet_a","toWalletId":"wallet_b","amount":100}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusUnprocessableEntity, resp.StatusCode, "HTTP status")

	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	assertEqual(t, "INSUFFICIENT_FUNDS", result.Error.Code, "error code")

	// Balance should be unchanged
	walletA := getWallet(t, "wallet_a")
	assertEqual(t, int64(50), walletA.Balance, "wallet_a balance unchanged")
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)

	body := `{"idempotencyKey":"tx-notfound-1","fromWalletId":"wallet_a","toWalletId":"nonexistent","amount":100}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusNotFound, resp.StatusCode, "HTTP status")
}

func TestCreateTransfer_SelfTransfer(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)

	body := `{"idempotencyKey":"tx-self-1","fromWalletId":"wallet_a","toWalletId":"wallet_a","amount":100}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")
}

func TestCreateTransfer_InvalidRequest_MissingFields(t *testing.T) {
	body := `{"idempotencyKey":"tx-invalid"}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")
}

func TestCreateTransfer_InvalidRequest_BadJSON(t *testing.T) {
	resp := doHTTPPost(t, "/transfers", `{invalid json}`)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")
}

func TestCreateTransfer_InvalidRequest_ZeroAmount(t *testing.T) {
	body := `{"idempotencyKey":"tx-zero","fromWalletId":"w1","toWalletId":"w2","amount":0}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")
}

func TestCreateTransfer_InvalidRequest_NegativeAmount(t *testing.T) {
	body := `{"idempotencyKey":"tx-neg","fromWalletId":"w1","toWalletId":"w2","amount":-100}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")

	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error == nil {
		t.Fatal("expected error response")
	}
	assertEqual(t, "INVALID_INPUT", result.Error.Code, "error code")
}

// ── Idempotency Tests ─────────────────────────────────────────────────────────

func TestIdempotency_DuplicateReturnsSameResult(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	// First request — should return 201 Created
	transfer1 := doTransfer(t, "tx-idem-1", "wallet_a", "wallet_b", 100, http.StatusCreated)

	// Second request with same idempotency key — should return 200 OK (not 201)
	transfer2 := doTransfer(t, "tx-idem-1", "wallet_a", "wallet_b", 100, http.StatusOK)

	assertEqual(t, transfer1.ID, transfer2.ID, "transfer ID should be the same")
	assertEqual(t, transfer1.Status, transfer2.Status, "transfer status should be the same")

	// Balance should only be debited ONCE
	walletA := getWallet(t, "wallet_a")
	assertEqual(t, int64(900), walletA.Balance, "wallet_a balance (debited once)")
}

func TestIdempotency_NoDuplicateLedgerEntries(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 0)

	transfer := doTransfer(t, "tx-idem-ledger", "wallet_a", "wallet_b", 200, http.StatusCreated)

	// Send duplicate — should return 200 OK
	doTransfer(t, "tx-idem-ledger", "wallet_a", "wallet_b", 200, http.StatusOK)

	// Should still have exactly 2 ledger entries
	var count int
	testDB.Pool.QueryRow(context.Background(),
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1::uuid", transfer.ID,
	).Scan(&count)
	assertEqual(t, 2, count, "ledger entry count after duplicate")
}

func TestIdempotency_MismatchedRequest(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)
	createTestWallet(t, "wallet_c", 500)

	// First request
	doTransfer(t, "tx-mismatch", "wallet_a", "wallet_b", 100, http.StatusCreated)

	// Same key, different parameters
	body := `{"idempotencyKey":"tx-mismatch","fromWalletId":"wallet_a","toWalletId":"wallet_c","amount":200}`
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusUnprocessableEntity, resp.StatusCode, "HTTP status for mismatched idempotency key")

	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error == nil {
		t.Fatal("expected error for mismatched idempotency key")
	}
	assertEqual(t, "IDEMPOTENCY_KEY_MISMATCH", result.Error.Code, "error code")
}

func TestIdempotency_FailedRequest(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 50)
	createTestWallet(t, "wallet_b", 100)

	body := `{"idempotencyKey":"tx-failed-idem","fromWalletId":"wallet_a","toWalletId":"wallet_b","amount":100}`

	// First request - fails with 422 Unprocessable Entity
	resp1 := doHTTPPost(t, "/transfers", body)
	defer resp1.Body.Close()
	assertStatusCode(t, http.StatusUnprocessableEntity, resp1.StatusCode, "First request status")

	var result1 apiResponse
	json.NewDecoder(resp1.Body).Decode(&result1)
	if result1.Error == nil {
		t.Fatal("expected error response")
	}
	assertEqual(t, "INSUFFICIENT_FUNDS", result1.Error.Code, "First request error code")

	// Second request (replay) - must return the exact same 422 Unprocessable Entity and error code
	resp2 := doHTTPPost(t, "/transfers", body)
	defer resp2.Body.Close()
	assertStatusCode(t, http.StatusUnprocessableEntity, resp2.StatusCode, "Second request status")

	var result2 apiResponse
	json.NewDecoder(resp2.Body).Decode(&result2)
	if result2.Error == nil {
		t.Fatal("expected error response on replay")
	}
	assertEqual(t, "INSUFFICIENT_FUNDS", result2.Error.Code, "Replay request error code")
}

// ── Transfer Retrieval Tests ──────────────────────────────────────────────────

func TestGetTransfer_Success(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	created := doTransfer(t, "tx-get-1", "wallet_a", "wallet_b", 100, http.StatusCreated)

	resp := doHTTPGet(t, "/transfers/"+created.ID)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusOK, resp.StatusCode, "HTTP status")

	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	var retrieved transferData
	json.Unmarshal(result.Data, &retrieved)

	assertEqual(t, created.ID, retrieved.ID, "transfer ID")
	assertEqual(t, "PROCESSED", retrieved.Status, "transfer status")
}

func TestGetTransfer_NotFound(t *testing.T) {
	resp := doHTTPGet(t, "/transfers/00000000-0000-0000-0000-000000000000")
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusNotFound, resp.StatusCode, "HTTP status")
}

func TestGetTransfer_InvalidUUID(t *testing.T) {
	resp := doHTTPGet(t, "/transfers/not-a-uuid")
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusBadRequest, resp.StatusCode, "HTTP status")
}

func TestGetTransfer_UppercaseUUID(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	created := doTransfer(t, "tx-get-upper", "wallet_a", "wallet_b", 100, http.StatusCreated)

	// Convert created ID to uppercase
	upperID := strings.ToUpper(created.ID)

	resp := doHTTPGet(t, "/transfers/"+upperID)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusOK, resp.StatusCode, "HTTP status")

	var result apiResponse
	json.NewDecoder(resp.Body).Decode(&result)
	var retrieved transferData
	json.Unmarshal(result.Data, &retrieved)

	assertEqual(t, created.ID, strings.ToLower(retrieved.ID), "transfer ID")
}

func TestIdempotencyRepo_MarkCompleted_KeyNotFound(t *testing.T) {
	cleanupDB(t)
	repo := repository.NewIdempotencyRepository()
	ctx := context.Background()

	err := repo.MarkCompleted(ctx, testDB.Pool, "non-existent-key", "00000000-0000-0000-0000-000000000001")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertEqual(t, domain.ErrIdempotencyKeyNotFound.Error(), err.Error(), "error message")
}

func TestIdempotencyRepo_MarkFailed_KeyNotFound(t *testing.T) {
	cleanupDB(t)
	repo := repository.NewIdempotencyRepository()
	ctx := context.Background()

	err := repo.MarkFailed(ctx, testDB.Pool, "non-existent-key", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	assertEqual(t, domain.ErrIdempotencyKeyNotFound.Error(), err.Error(), "error message")
}

// ── Wallet Tests ──────────────────────────────────────────────────────────────

func TestCreateWallet_Success(t *testing.T) {
	cleanupDB(t)

	body := `{"id":"new_wallet","balance":500,"currency":"EUR"}`
	resp := doHTTPPost(t, "/wallets", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusCreated, resp.StatusCode, "HTTP status")

	wallet := getWallet(t, "new_wallet")
	assertEqual(t, int64(500), wallet.Balance, "wallet balance")
	assertEqual(t, "EUR", wallet.Currency, "wallet currency")
}

func TestCreateWallet_Duplicate(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_dup", 100)

	body := `{"id":"wallet_dup","balance":200,"currency":"USD"}`
	resp := doHTTPPost(t, "/wallets", body)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusConflict, resp.StatusCode, "HTTP status for duplicate wallet")
}

func TestGetWallet_NotFound(t *testing.T) {
	cleanupDB(t)

	resp := doHTTPGet(t, "/wallets/nonexistent")
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusNotFound, resp.StatusCode, "HTTP status")
}

// ── Multiple Transfers Test ───────────────────────────────────────────────────

func TestMultipleTransfers_BalanceConsistency(t *testing.T) {
	cleanupDB(t)
	createTestWallet(t, "wallet_a", 1000)
	createTestWallet(t, "wallet_b", 500)

	// Transfer 1: A → B (200)
	doTransfer(t, "multi-1", "wallet_a", "wallet_b", 200, http.StatusCreated)
	// Transfer 2: B → A (100)
	doTransfer(t, "multi-2", "wallet_b", "wallet_a", 100, http.StatusCreated)
	// Transfer 3: A → B (300)
	doTransfer(t, "multi-3", "wallet_a", "wallet_b", 300, http.StatusCreated)

	walletA := getWallet(t, "wallet_a")
	walletB := getWallet(t, "wallet_b")

	// A: 1000 - 200 + 100 - 300 = 600
	// B: 500 + 200 - 100 + 300 = 900
	assertEqual(t, int64(600), walletA.Balance, "wallet_a balance after 3 transfers")
	assertEqual(t, int64(900), walletB.Balance, "wallet_b balance after 3 transfers")

	// Total money in system should be conserved: 1000 + 500 = 1500
	assertEqual(t, int64(1500), walletA.Balance+walletB.Balance, "total money conserved")
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func doTransfer(t *testing.T, key, from, to string, amount int64, expectedStatus int) transferData {
	t.Helper()
	body := fmt.Sprintf(
		`{"idempotencyKey":"%s","fromWalletId":"%s","toWalletId":"%s","amount":%d}`,
		key, from, to, amount,
	)
	resp := doHTTPPost(t, "/transfers", body)
	defer resp.Body.Close()

	assertStatusCode(t, expectedStatus, resp.StatusCode, "HTTP status")

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected successful response, got error: %+v", result.Error)
	}

	var transfer transferData
	if err := json.Unmarshal(result.Data, &transfer); err != nil {
		t.Fatalf("failed to unmarshal transfer: %v", err)
	}
	return transfer
}

func getWallet(t *testing.T, id string) walletData {
	t.Helper()
	resp := doHTTPGet(t, "/wallets/"+id)
	defer resp.Body.Close()

	assertStatusCode(t, http.StatusOK, resp.StatusCode, "HTTP status for get wallet")

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode wallet response: %v", err)
	}

	var wallet walletData
	if err := json.Unmarshal(result.Data, &wallet); err != nil {
		t.Fatalf("failed to unmarshal wallet: %v", err)
	}
	return wallet
}

func doHTTPPost(t *testing.T, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(testServer.URL+path, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

func doHTTPGet(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(testServer.URL + path)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	return resp
}

// assertStatusCode uses t.Fatalf for HTTP status code mismatches, since continuing
// a test after a wrong status code produces misleading secondary errors.
func assertStatusCode(t *testing.T, expected, actual int, label string) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %d, got %d", label, expected, actual)
	}
}

// assertEqual uses t.Errorf for non-critical assertions, allowing the test to
// report multiple failures for better diagnostics.
func assertEqual[T comparable](t *testing.T, expected, actual T, label string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", label, expected, actual)
	}
}
