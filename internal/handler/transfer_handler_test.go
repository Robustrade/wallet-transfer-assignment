package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/service"
)

// fakeService implements transferCreator for handler tests.
type fakeService struct {
	tr      *domain.Transfer
	created bool
	err     error
}

func (f *fakeService) CreateTransfer(ctx context.Context, req service.TransferRequest) (*domain.Transfer, bool, error) {
	return f.tr, f.created, f.err
}

func doRequest(t *testing.T, svc transferCreator, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := NewTransferHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	return rec
}

func TestHandler_Created(t *testing.T) {
	svc := &fakeService{
		tr:      &domain.Transfer{ID: "t1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100, Status: domain.StatusProcessed},
		created: true,
	}
	rec := doRequest(t, svc, `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
}

func TestHandler_DuplicateReturnsOK(t *testing.T) {
	svc := &fakeService{
		tr:      &domain.Transfer{ID: "t1", Status: domain.StatusProcessed},
		created: false,
	}
	rec := doRequest(t, svc, `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandler_BadJSON(t *testing.T) {
	rec := doRequest(t, &fakeService{}, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_MissingIdempotencyKey(t *testing.T) {
	rec := doRequest(t, &fakeService{}, `{"fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_ErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"invalid amount", domain.ErrInvalidAmount, http.StatusBadRequest},
		{"same wallet", domain.ErrSameWallet, http.StatusBadRequest},
		{"wallet not found", repository.ErrWalletNotFound, http.StatusNotFound},
		{"insufficient balance", service.ErrInsufficientBalance, http.StatusUnprocessableEntity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{err: tt.err}
			rec := doRequest(t, svc, `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}