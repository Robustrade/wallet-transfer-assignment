package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func TestCreateTransferEndpointReturnsOriginalResultOnReplay(t *testing.T) {
	ctx := context.Background()
	transferService := newHTTPTestService(t)
	mustCreateHTTPTestWallet(t, transferService, "wallet_1", 500)
	mustCreateHTTPTestWallet(t, transferService, "wallet_2", 0)

	server := httpapi.NewServer(transferService, log.New(io.Discard, "", 0))
	body := []byte(`{"idempotencyKey":"http-replay","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":125}`)

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequestWithContext(ctx, http.MethodPost, "/transfers", bytes.NewReader(body))
	server.ServeHTTP(first, firstRequest)

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequestWithContext(ctx, http.MethodPost, "/transfers", bytes.NewReader(body))
	server.ServeHTTP(second, secondRequest)

	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want %d; body: %s", first.Code, http.StatusCreated, first.Body.String())
	}
	if second.Code != first.Code {
		t.Fatalf("replay status = %d, want %d", second.Code, first.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("replay body differs\nfirst: %s\nsecond: %s", first.Body.String(), second.Body.String())
	}
}

func TestCreateWalletEndpointUsesJSONFieldNameInValidationError(t *testing.T) {
	ctx := context.Background()
	server := httpapi.NewServer(newHTTPTestService(t), log.New(io.Discard, "", 0))
	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"/wallets",
		bytes.NewReader([]byte(`{"id":"","initialBalance":100}`)),
	)

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}

	var body struct {
		Error struct {
			Code    domain.ErrorCode `json:"code"`
			Message string           `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Error.Code != domain.ErrorInvalidRequest {
		t.Fatalf("error code = %s, want %s", body.Error.Code, domain.ErrorInvalidRequest)
	}
	if body.Error.Message != "id is required" {
		t.Fatalf("error message = %q, want %q", body.Error.Message, "id is required")
	}
}

func newHTTPTestService(t *testing.T) *service.TransferService {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "wallet.db")
	store, err := repository.OpenSQLite(ctx, fmt.Sprintf("file:%s?_busy_timeout=5000&_foreign_keys=on", dbPath))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	return service.NewTransferService(store)
}

func mustCreateHTTPTestWallet(t *testing.T, transferService *service.TransferService, id string, balance int64) {
	t.Helper()

	_, err := transferService.CreateWallet(context.Background(), domain.CreateWalletInput{
		ID:             id,
		InitialBalance: balance,
	})
	if err != nil {
		t.Fatalf("CreateWallet(%s) returned error: %v", id, err)
	}
}
