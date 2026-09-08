package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	httphandler "github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/handler/http"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

type fakeCreator struct {
	result service.TransferResult
	err    error
	gotCmd service.CreateTransferCommand
	calls  int
}

func (f *fakeCreator) CreateTransfer(_ context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error) {
	f.calls++
	f.gotCmd = cmd
	return f.result, f.err
}

func processed(replayed bool) service.TransferResult {
	return service.TransferResult{
		Transfer: &domain.Transfer{ID: "txn_1", IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b",
			Amount: domain.Money{Amount: 100, Currency: "USD"}, State: domain.TransferProcessed},
		Replayed: replayed,
	}
}

func doPost(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const validBody = `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":100}`

func router(c httphandler.TransferCreator) http.Handler {
	return httphandler.NewRouter(httphandler.Deps{Transfers: c, Wallets: nil})
}

func TestTransferHandler_Created(t *testing.T) {
	t.Parallel()
	f := &fakeCreator{result: processed(false)}
	rec := doPost(t, router(f), validBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if f.gotCmd.IdempotencyKey != "k" || f.gotCmd.Amount != 100 {
		t.Fatalf("command not mapped: %+v", f.gotCmd)
	}
	if !strings.Contains(rec.Body.String(), `"state":"PROCESSED"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestTransferHandler_ReplayReturns200(t *testing.T) {
	t.Parallel()
	rec := doPost(t, router(&fakeCreator{result: processed(true)}), validBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestTransferHandler_FailedReturns422(t *testing.T) {
	t.Parallel()
	res := service.TransferResult{Transfer: &domain.Transfer{
		ID: "txn_1", Amount: domain.Money{Amount: 100, Currency: "USD"},
		State: domain.TransferFailed, FailureReason: "insufficient funds",
	}}
	rec := doPost(t, router(&fakeCreator{result: res}), validBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "insufficient funds") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestTransferHandler_BadRequests(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"malformed json": `{"idempotencyKey":`,
		"unknown field":  `{"idempotencyKey":"k","fromWalletId":"a","toWalletId":"b","amount":1,"extra":true}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := &fakeCreator{result: processed(false)}
			rec := doPost(t, router(f), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if f.calls != 0 {
				t.Fatal("service should not be called on a bad request")
			}
		})
	}
}

func TestTransferHandler_ErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"validation", domain.ErrValidation, http.StatusBadRequest},
		{"wallet not found", domain.ErrWalletNotFound, http.StatusNotFound},
		{"idempotency conflict", domain.ErrIdempotencyKeyConflict, http.StatusConflict},
		{"insufficient funds", domain.ErrInsufficientFunds, http.StatusUnprocessableEntity},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := doPost(t, router(&fakeCreator{err: tc.err}), validBody)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestTransferHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	rec := httptest.NewRecorder()
	router(&fakeCreator{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
