package transfer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetricsExposeHTTPAndTransferMetrics(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	service := NewService(store)
	server := NewHTTPServer(service)
	handler := server.Handler()

	request := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"idempotencyKey":"metrics-1","fromWalletId":"source","toWalletId":"destination","amount":10}`))
	request = request.WithContext(context.Background())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("transfer status = %d, want %d", response.Code, http.StatusCreated)
	}

	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	metrics := metricsResponse.Body.String()
	for _, metric := range []string{
		"wallet_transfer_http_requests_total",
		"wallet_transfer_http_request_duration_seconds",
		"wallet_transfer_transfers_total{outcome=\"processed\"}",
	} {
		if !strings.Contains(metrics, metric) {
			t.Fatalf("metrics output does not contain %q", metric)
		}
	}
}

func TestGetAllLedgers(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	service := NewService(store)
	transfer, _, err := service.Transfer(context.Background(), TransferRequest{
		IdempotencyKey: "ledger-api-1",
		FromWalletID:   "source",
		ToWalletID:     "destination",
		AmountCents:    10,
	})
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	NewHTTPServer(service).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ledgers", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ledger status = %d, want %d", response.Code, http.StatusOK)
	}

	var entries []LedgerEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].TransferID != transfer.ID || entries[1].TransferID != transfer.ID {
		t.Fatalf("ledger entries = %#v", entries)
	}
}

func TestCreateTransferReplaysDuplicateRequestWithoutDuplicateSideEffects(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	handler := NewHTTPServer(NewService(store)).Handler()
	body := `{"idempotencyKey":" duplicate-api-1 ","fromWalletId":" source ","toWalletId":" destination ","amount":10}`

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)))
	if first.Code != http.StatusCreated {
		t.Fatalf("first transfer status = %d, want %d", first.Code, http.StatusCreated)
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)))
	if second.Code != http.StatusOK {
		t.Fatalf("duplicate transfer status = %d, want %d", second.Code, http.StatusOK)
	}

	var firstTransfer, secondTransfer Transfer
	if err := json.NewDecoder(first.Body).Decode(&firstTransfer); err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(second.Body).Decode(&secondTransfer); err != nil {
		t.Fatal(err)
	}
	if firstTransfer.ID != secondTransfer.ID {
		t.Fatalf("duplicate transfer IDs = %q and %q", firstTransfer.ID, secondTransfer.ID)
	}

	entries, err := store.GetAllLedgerEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
}

func TestCreateTransferRejectsChangedDuplicateRequest(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	handler := NewHTTPServer(NewService(store)).Handler()

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"idempotencyKey":"duplicate-api-2","fromWalletId":"source","toWalletId":"destination","amount":10}`)))
	if first.Code != http.StatusCreated {
		t.Fatalf("first transfer status = %d, want %d", first.Code, http.StatusCreated)
	}

	changed := httptest.NewRecorder()
	handler.ServeHTTP(changed, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(`{"idempotencyKey":"duplicate-api-2","fromWalletId":"source","toWalletId":"destination","amount":20}`)))
	if changed.Code != http.StatusConflict {
		t.Fatalf("changed duplicate status = %d, want %d", changed.Code, http.StatusConflict)
	}
}
