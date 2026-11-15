package controller_test

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

type transferBody struct {
	TransferID     string  `json:"transferId"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason"`
	CreatedAt      string  `json:"createdAt"`
}

type transferErrorBody struct {
	Error struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	} `json:"error"`
	Transfer *transferBody `json:"transfer"`
}

type ledgerRow struct {
	WalletID  string
	EntryType string
	Amount    int64
}

func createWallet(t *testing.T, id string, balance int64) {
	t.Helper()
	rec := postJSON(t, "/create-wallet", fmt.Sprintf(`{"walletId":%q,"initialBalance":%d}`, id, balance))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create wallet %s: status = %d, body: %s", id, rec.Code, rec.Body.String())
	}
}

func transferJSON(key, from, to string, amount int64) string {
	return fmt.Sprintf(`{"idempotencyKey":%q,"fromWalletId":%q,"toWalletId":%q,"amount":%d}`, key, from, to, amount)
}

func countRows(t *testing.T, table string) int64 {
	t.Helper()
	var count int64
	if err := database.DB.Raw(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}

func ledgerFor(t *testing.T, transferID string) []ledgerRow {
	t.Helper()
	var rows []ledgerRow
	err := database.DB.Raw(
		"SELECT wallet_id, entry_type, amount FROM ledger_entries WHERE transfer_id = ? ORDER BY id", transferID,
	).Scan(&rows).Error
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	return rows
}

func assertBalance(t *testing.T, walletID string, want int64) {
	t.Helper()
	if got, _ := storedBalance(t, walletID); got != want {
		t.Errorf("balance of %s = %d, want %d", walletID, got, want)
	}
}

func TestCreateTransfer_MovesMoneyAndWritesLedger(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 2000)
	createWallet(t, "wallet_2", 500)

	rec := postJSON(t, "/transfers", transferJSON("key-1", "wallet_1", "wallet_2", 300))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	body := decode[transferBody](t, rec)
	if body.TransferID == "" || body.Status != "PROCESSED" || body.Amount != 300 || body.FailureReason != nil {
		t.Errorf("response = %+v, want a PROCESSED transfer of 300 with an ID and no failure reason", body)
	}
	assertBalance(t, "wallet_1", 1700)
	assertBalance(t, "wallet_2", 800)

	entries := ledgerFor(t, body.TransferID)
	want := []ledgerRow{{"wallet_1", "DEBIT", 300}, {"wallet_2", "CREDIT", 300}}
	if len(entries) != 2 || entries[0] != want[0] || entries[1] != want[1] {
		t.Errorf("ledger entries = %+v, want %+v", entries, want)
	}
}

func TestCreateTransfer_InsufficientFundsRecordsFailedTransfer(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 100)
	createWallet(t, "wallet_2", 0)

	rec := postJSON(t, "/transfers", transferJSON("key-1", "wallet_1", "wallet_2", 500))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	body := decode[transferErrorBody](t, rec)
	if body.Error.Code != "INSUFFICIENT_FUNDS" {
		t.Errorf("error code = %q, want INSUFFICIENT_FUNDS", body.Error.Code)
	}
	if body.Transfer == nil || body.Transfer.Status != "FAILED" ||
		body.Transfer.FailureReason == nil || *body.Transfer.FailureReason != "INSUFFICIENT_FUNDS" {
		t.Errorf("transfer = %+v, want FAILED with reason INSUFFICIENT_FUNDS", body.Transfer)
	}
	assertBalance(t, "wallet_1", 100)
	assertBalance(t, "wallet_2", 0)
	if n := countRows(t, "ledger_entries"); n != 0 {
		t.Errorf("ledger entries = %d, want 0", n)
	}
	if n := countRows(t, "transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1 (the failed attempt is recorded)", n)
	}
}

func TestCreateTransfer_RetryReturnsOriginalResultWithoutMovingMoneyAgain(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 2000)
	createWallet(t, "wallet_2", 500)
	request := transferJSON("key-1", "wallet_1", "wallet_2", 300)

	first := postJSON(t, "/transfers", request)
	retry := postJSON(t, "/transfers", request)

	if first.Code != http.StatusCreated || retry.Code != http.StatusCreated {
		t.Fatalf("statuses = %d, %d, want 201, 201", first.Code, retry.Code)
	}
	if first.Header().Get("Idempotent-Replayed") != "" {
		t.Error("first response should not be marked as replayed")
	}
	if retry.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("retry response should have header Idempotent-Replayed: true")
	}
	if first.Body.String() != retry.Body.String() {
		t.Errorf("retry body differs from original:\nfirst: %s\nretry: %s", first.Body.String(), retry.Body.String())
	}
	assertBalance(t, "wallet_1", 1700)
	assertBalance(t, "wallet_2", 800)
	if n := countRows(t, "transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
	if n := countRows(t, "ledger_entries"); n != 2 {
		t.Errorf("ledger entries = %d, want 2", n)
	}
}

func TestCreateTransfer_RetryOfFailedTransferReturnsSameFailure(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 100)
	createWallet(t, "wallet_2", 0)
	request := transferJSON("key-1", "wallet_1", "wallet_2", 500)

	first := postJSON(t, "/transfers", request)
	retry := postJSON(t, "/transfers", request)

	if first.Code != http.StatusUnprocessableEntity || retry.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statuses = %d, %d, want 422, 422", first.Code, retry.Code)
	}
	if retry.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("retry response should have header Idempotent-Replayed: true")
	}
	if first.Body.String() != retry.Body.String() {
		t.Errorf("retry body differs from original:\nfirst: %s\nretry: %s", first.Body.String(), retry.Body.String())
	}
	if n := countRows(t, "transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
}

func TestCreateTransfer_SameKeyWithDifferentRequestIsRejected(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 2000)
	createWallet(t, "wallet_2", 500)
	postJSON(t, "/transfers", transferJSON("key-1", "wallet_1", "wallet_2", 300))

	rec := postJSON(t, "/transfers", transferJSON("key-1", "wallet_1", "wallet_2", 400))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
	if code := decode[transferErrorBody](t, rec).Error.Code; code != "IDEMPOTENCY_KEY_REUSED" {
		t.Errorf("error code = %q, want IDEMPOTENCY_KEY_REUSED", code)
	}
	assertBalance(t, "wallet_1", 1700)
	assertBalance(t, "wallet_2", 800)
	if n := countRows(t, "transfers"); n != 1 {
		t.Errorf("transfers = %d, want 1", n)
	}
}

func TestCreateTransfer_UnknownWalletDoesNotConsumeKey(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 2000)
	request := transferJSON("key-1", "wallet_1", "wallet_2", 300)

	rec := postJSON(t, "/transfers", request)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if code := decode[transferErrorBody](t, rec).Error.Code; code != "WALLET_NOT_FOUND" {
		t.Errorf("error code = %q, want WALLET_NOT_FOUND", code)
	}
	if n := countRows(t, "idempotency_records"); n != 0 {
		t.Errorf("idempotency records = %d, want 0 (key must not be consumed)", n)
	}
	if n := countRows(t, "transfers"); n != 0 {
		t.Errorf("transfers = %d, want 0", n)
	}

	createWallet(t, "wallet_2", 0)
	retry := postJSON(t, "/transfers", request)

	if retry.Code != http.StatusCreated {
		t.Fatalf("retry after creating wallet: status = %d, want 201; body: %s", retry.Code, retry.Body.String())
	}
	if retry.Header().Get("Idempotent-Replayed") != "" {
		t.Error("retry should execute fresh, not be marked as replayed")
	}
}

func TestCreateTransfer_CreditOverflowIsRejectedWithoutChanges(t *testing.T) {
	testutil.ResetDatabase(t)
	createWallet(t, "wallet_1", 100)
	createWallet(t, "full_wallet", math.MaxInt64)

	rec := postJSON(t, "/transfers", transferJSON("key-1", "wallet_1", "full_wallet", 1))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	body := decode[transferErrorBody](t, rec)
	if body.Error.Code != "VALIDATION_ERROR" || body.Error.Field != "amount" {
		t.Errorf("error = %+v, want VALIDATION_ERROR on field amount", body.Error)
	}
	assertBalance(t, "wallet_1", 100)
	assertBalance(t, "full_wallet", math.MaxInt64)
	if n := countRows(t, "transfers"); n != 0 {
		t.Errorf("transfers = %d, want 0 (the transaction must roll back)", n)
	}
	if n := countRows(t, "idempotency_records"); n != 0 {
		t.Errorf("idempotency records = %d, want 0 (key must not be consumed)", n)
	}
}

func TestCreateTransfer_InvalidRequestsAreRejected(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{"missing idempotency key", `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`, "idempotencyKey"},
		{"missing source wallet", `{"idempotencyKey":"k","toWalletId":"wallet_2","amount":100}`, "fromWalletId"},
		{"missing destination wallet", `{"idempotencyKey":"k","fromWalletId":"wallet_1","amount":100}`, "toWalletId"},
		{"zero amount", transferJSON("k", "wallet_1", "wallet_2", 0), "amount"},
		{"negative amount", transferJSON("k", "wallet_1", "wallet_2", -5), "amount"},
		{"same source and destination", transferJSON("k", "wallet_1", "wallet_1", 100), "toWalletId"},
		{"amount as text", `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":"100"}`, ""},
		{"fractional amount", `{"idempotencyKey":"k","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":1.5}`, ""},
		{"malformed JSON", `{"idempotencyKey":`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.ResetDatabase(t)
			createWallet(t, "wallet_1", 2000)
			createWallet(t, "wallet_2", 500)

			rec := postJSON(t, "/transfers", tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			body := decode[transferErrorBody](t, rec)
			if body.Error.Code != "VALIDATION_ERROR" || body.Error.Field != tc.wantField {
				t.Errorf("error = %+v, want VALIDATION_ERROR on field %q", body.Error, tc.wantField)
			}
			if n := countRows(t, "transfers"); n != 0 {
				t.Errorf("transfers = %d, want 0", n)
			}
			if n := countRows(t, "idempotency_records"); n != 0 {
				t.Errorf("idempotency records = %d, want 0", n)
			}
		})
	}
}
