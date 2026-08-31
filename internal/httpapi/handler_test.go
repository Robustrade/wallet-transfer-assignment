package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/metrics"
)

type fakeService struct {
	transfer   domain.Transfer
	createErr  error
	balance    int64
	balanceErr error
	history    []domain.Transfer
	historyErr error
}

func (f fakeService) CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error) {
	return f.transfer, f.createErr
}
func (f fakeService) Balance(context.Context, string) (int64, error) { return f.balance, f.balanceErr }
func (f fakeService) TransferHistory(context.Context, string, int) ([]domain.Transfer, error) {
	return f.history, f.historyErr
}
func testHandler(fake fakeService) http.Handler {
	return New(fake, fake, &metrics.Collector{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()
}

// TestWalletBalanceReturnsJSONAndRequestID verifies the balance endpoint's transport contract.
func TestWalletBalanceReturnsJSONAndRequestID(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.NewString()+"/balance", nil)
	recorder := httptest.NewRecorder()
	testHandler(fakeService{balance: 125}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"balance":125`) || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status=%d body=%s headers=%v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

// TestTransferHistoryRejectsBadLimit verifies history pagination is bounded at the HTTP boundary.
func TestTransferHistoryRejectsBadLimit(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.NewString()+"/transfers?limit=invalid", nil)
	recorder := httptest.NewRecorder()
	testHandler(fakeService{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}

// TestCreateTransferMapsInsufficientFundsToUnprocessableEntity verifies domain error transport mapping.
func TestCreateTransferMapsInsufficientFundsToUnprocessableEntity(t *testing.T) {
	from, to := uuid.NewString(), uuid.NewString()
	body := `{"idempotencyKey":"key","fromWalletId":"` + from + `","toWalletId":"` + to + `","amount":10}`
	recorder := httptest.NewRecorder()
	testHandler(fakeService{transfer: domain.Transfer{Status: domain.Failed}, createErr: domain.ErrInsufficientFunds}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body)))
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestCreateTransferCoversSuccessfulAndClientErrorPaths verifies request decoding and response mapping.
func TestCreateTransferCoversSuccessfulAndClientErrorPaths(t *testing.T) {
	from, to := uuid.NewString(), uuid.NewString()
	validBody := `{"idempotencyKey":"key","fromWalletId":"` + from + `","toWalletId":"` + to + `","amount":10}`
	tests := []struct {
		name    string
		body    string
		service fakeService
		status  int
	}{
		{"invalid json", "{", fakeService{}, http.StatusBadRequest},
		{"success", validBody, fakeService{transfer: domain.Transfer{ID: uuid.NewString(), Status: domain.Processed}}, http.StatusCreated},
		{"conflict", validBody, fakeService{createErr: domain.ErrIdempotencyConflict}, http.StatusConflict},
		{"missing wallet", validBody, fakeService{createErr: domain.ErrWalletNotFound}, http.StatusNotFound},
		{"unexpected error", validBody, fakeService{createErr: errors.New("database unavailable")}, http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			testHandler(test.service).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(test.body)))
			if recorder.Code != test.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestTransferHistoryReturnsResultsAndDomainErrors verifies history result and error mapping.
func TestTransferHistoryReturnsResultsAndDomainErrors(t *testing.T) {
	walletID := uuid.NewString()
	recorder := httptest.NewRecorder()
	testHandler(fakeService{history: []domain.Transfer{{ID: uuid.NewString()}}}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/wallets/"+walletID+"/transfers?limit=1", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "transfers") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	testHandler(fakeService{historyErr: domain.ErrWalletNotFound}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/wallets/"+walletID+"/transfers", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	testHandler(fakeService{historyErr: errors.New("database unavailable")}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/wallets/"+walletID+"/transfers", nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d", recorder.Code)
	}
}

// TestHandlerUsesDefaultDependencies verifies nil optional observability dependencies are safe.
func TestHandlerUsesDefaultDependencies(t *testing.T) {
	handler := New(fakeService{}, fakeService{}, nil, nil).Routes()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
}

// TestWalletBalanceMapsMissingWalletToNotFound verifies typed query errors are exposed safely.
func TestWalletBalanceMapsMissingWalletToNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler(fakeService{balanceErr: domain.ErrWalletNotFound}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/wallets/"+uuid.NewString()+"/balance", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d", recorder.Code)
	}
}

// TestFakeServiceErrorsStayComparable verifies the fake is configured with sentinel domain errors.
func TestFakeServiceErrorsStayComparable(t *testing.T) {
	if !errors.Is(fakeService{createErr: domain.ErrInsufficientFunds}.createErr, domain.ErrInsufficientFunds) {
		t.Fatal("sentinel error comparison failed")
	}
}
