package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/handler"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/service"
)

type fakeTransferService struct {
	transfer domain.Transfer
	err      error
}

func (f *fakeTransferService) CreateTransfer(_ context.Context, _ service.CreateTransferRequest) (domain.Transfer, error) {
	return f.transfer, f.err
}

func doRequest(t *testing.T, h *handler.TransferHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	return rec
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"idempotencyKey":`},
		{name: "non-numeric amount", body: `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":"abc"}`},
		{name: "missing idempotencyKey", body: `{"fromWalletId":"a","toWalletId":"b","amount":100}`},
		{name: "missing fromWalletId", body: `{"idempotencyKey":"k1","toWalletId":"b","amount":100}`},
		{name: "missing toWalletId", body: `{"idempotencyKey":"k1","fromWalletId":"a","amount":100}`},
		{name: "same wallet", body: `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"a","amount":100}`},
		{name: "zero amount", body: `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":0}`},
		{name: "negative amount", body: `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":-1}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handler.NewTransferHandler(&fakeTransferService{})
			rec := doRequest(t, h, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateTransfer_ServiceErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "wallet not found", err: domain.ErrWalletNotFound, wantStatus: http.StatusNotFound},
		{name: "idempotency conflict", err: domain.ErrIdempotencyKeyConflict, wantStatus: http.StatusConflict},
		{name: "unexpected error", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}

	const validBody = `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handler.NewTransferHandler(&fakeTransferService{err: tc.err})
			rec := doRequest(t, h, validBody)
			if rec.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tc.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateTransfer_Success(t *testing.T) {
	transferID := uuid.New()
	h := handler.NewTransferHandler(&fakeTransferService{
		transfer: domain.Transfer{
			ID:           transferID,
			FromWalletID: "wallet_1",
			ToWalletID:   "wallet_2",
			Amount:       100,
			Status:       domain.StatusProcessed,
			CreatedAt:    time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
		},
	})

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["transferId"] != transferID.String() {
		t.Errorf("expected transferId %s, got %v", transferID, got["transferId"])
	}
	if got["status"] != "PROCESSED" {
		t.Errorf("expected status PROCESSED, got %v", got["status"])
	}
	if got["fromWalletId"] != "wallet_1" || got["toWalletId"] != "wallet_2" {
		t.Errorf("expected wallet_1->wallet_2, got %v->%v", got["fromWalletId"], got["toWalletId"])
	}
	if got["failureReason"] != nil {
		t.Errorf("expected failureReason null, got %v", got["failureReason"])
	}
}

// TestCreateTransfer_BusinessFailure_Returns201 pins down design.md's
// explicit note: a business failure (e.g. insufficient balance) is still a
// successfully-created transfer resource. The outcome lives in the
// status/failureReason fields, not the HTTP status code.
func TestCreateTransfer_BusinessFailure_Returns201(t *testing.T) {
	reason := "insufficient balance"
	h := handler.NewTransferHandler(&fakeTransferService{
		transfer: domain.Transfer{
			ID:            uuid.New(),
			FromWalletID:  "wallet_1",
			ToWalletID:    "wallet_2",
			Amount:        100,
			Status:        domain.StatusFailed,
			FailureReason: &reason,
			CreatedAt:     time.Now(),
		},
	})

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"wallet_1","toWalletId":"wallet_2","amount":100}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 even for a business failure, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["status"] != "FAILED" {
		t.Errorf("expected status FAILED, got %v", got["status"])
	}
	if got["failureReason"] != reason {
		t.Errorf("expected failureReason %q, got %v", reason, got["failureReason"])
	}
}
