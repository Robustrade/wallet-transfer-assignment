package controller_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/router"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithDatabase(m))
}

type walletBody struct {
	WalletID  string `json:"walletId"`
	Balance   int64  `json:"balance"`
	CreatedAt string `json:"createdAt"`
}

type errorBody struct {
	Error struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	} `json:"error"`
}

func postJSON(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	router.SetupRoutes(r)

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return v
}

func storedBalance(t *testing.T, walletID string) (int64, bool) {
	t.Helper()
	var balances []int64
	if err := database.DB.Raw("SELECT balance FROM wallets WHERE id = ?", walletID).Scan(&balances).Error; err != nil {
		t.Fatalf("query wallet: %v", err)
	}
	if len(balances) == 0 {
		return 0, false
	}
	return balances[0], true
}

func walletCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := database.DB.Raw("SELECT COUNT(*) FROM wallets").Scan(&count).Error; err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	return count
}

func TestCreateWallet_Success(t *testing.T) {
	testutil.ResetDatabase(t)

	rec := postJSON(t, "/create-wallet", `{"walletId":"wallet_1","initialBalance":2000}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	body := decode[walletBody](t, rec)
	if body.WalletID != "wallet_1" || body.Balance != 2000 {
		t.Errorf("response = %+v, want wallet_1 with balance 2000", body)
	}
	if body.CreatedAt == "" {
		t.Error("createdAt is empty")
	}
	if balance, ok := storedBalance(t, "wallet_1"); !ok || balance != 2000 {
		t.Errorf("stored wallet: found=%v balance=%d, want found with balance 2000", ok, balance)
	}
}

func TestCreateWallet_ZeroBalanceIsAllowed(t *testing.T) {
	testutil.ResetDatabase(t)

	rec := postJSON(t, "/create-wallet", `{"walletId":"empty_wallet","initialBalance":0}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateWallet_DuplicateIDIsRejected(t *testing.T) {
	testutil.ResetDatabase(t)
	postJSON(t, "/create-wallet", `{"walletId":"wallet_1","initialBalance":2000}`)

	rec := postJSON(t, "/create-wallet", `{"walletId":"wallet_1","initialBalance":500}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", rec.Code, rec.Body.String())
	}
	if code := decode[errorBody](t, rec).Error.Code; code != "WALLET_ALREADY_EXISTS" {
		t.Errorf("error code = %q, want WALLET_ALREADY_EXISTS", code)
	}
	if balance, _ := storedBalance(t, "wallet_1"); balance != 2000 {
		t.Errorf("stored balance = %d, want original 2000 to be unchanged", balance)
	}
}

func TestCreateWallet_InvalidRequestsAreRejected(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{"negative balance", `{"walletId":"wallet_1","initialBalance":-1}`, "initialBalance"},
		{"missing balance", `{"walletId":"wallet_1"}`, "initialBalance"},
		{"missing wallet id", `{"initialBalance":100}`, "walletId"},
		{"whitespace-only wallet id", `{"walletId":"   ","initialBalance":100}`, "walletId"},
		{"balance as text", `{"walletId":"wallet_1","initialBalance":"100"}`, ""},
		{"fractional balance", `{"walletId":"wallet_1","initialBalance":10.5}`, ""},
		{"malformed JSON", `{"walletId":`, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.ResetDatabase(t)

			rec := postJSON(t, "/create-wallet", tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			body := decode[errorBody](t, rec)
			if body.Error.Code != "VALIDATION_ERROR" {
				t.Errorf("error code = %q, want VALIDATION_ERROR", body.Error.Code)
			}
			if body.Error.Field != tc.wantField {
				t.Errorf("error field = %q, want %q", body.Error.Field, tc.wantField)
			}
			if count := walletCount(t); count != 0 {
				t.Errorf("wallet count = %d, want 0 (nothing should be saved)", count)
			}
		})
	}
}
