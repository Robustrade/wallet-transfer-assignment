package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/service"
)

// fakeTransferCreator lets us test HTTP-layer concerns (status codes,
// JSON shape, error mapping) in complete isolation from the database.
type fakeTransferCreator struct {
	result service.TransferResult
	err    error
}

func (f *fakeTransferCreator) CreateTransfer(ctx context.Context, req domain.NewTransferRequest) (service.TransferResult, error) {
	return f.result, f.err
}

func doRequest(t *testing.T, h *TransferHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.CreateTransfer(rec, req)
	return rec
}

func TestCreateTransfer_Success_Returns201(t *testing.T) {
	fake := &fakeTransferCreator{result: service.TransferResult{
		Transfer: domain.Transfer{ID: "t1", Status: domain.TransferProcessed, FromWalletID: "a", ToWalletID: "b", Amount: 100},
	}}
	h := NewTransferHandler(fake)

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d. body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	var resp transferResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TransferID != "t1" || resp.Status != "PROCESSED" || resp.Replayed {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestCreateTransfer_Replayed_Returns200(t *testing.T) {
	fake := &fakeTransferCreator{result: service.TransferResult{
		Transfer: domain.Transfer{ID: "t1", Status: domain.TransferProcessed},
		Replayed: true,
	}}
	h := NewTransferHandler(fake)

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCreateTransfer_InsufficientFunds_Returns422(t *testing.T) {
	fake := &fakeTransferCreator{err: domain.ErrInsufficientFunds}
	h := NewTransferHandler(fake)

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCreateTransfer_IdempotencyConflict_Returns409(t *testing.T) {
	fake := &fakeTransferCreator{err: domain.ErrIdempotencyConflict}
	h := NewTransferHandler(fake)

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestCreateTransfer_ValidationErrors_Return400(t *testing.T) {
	cases := []error{domain.ErrSameWallet, domain.ErrInvalidAmount, domain.ErrIdempotencyKeyEmpty, domain.ErrWalletNotFound}
	for _, wantErr := range cases {
		fake := &fakeTransferCreator{err: wantErr}
		h := NewTransferHandler(fake)
		rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%v: status = %d, want %d", wantErr, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestCreateTransfer_MalformedJSON_Returns400(t *testing.T) {
	h := NewTransferHandler(&fakeTransferCreator{})
	rec := doRequest(t, h, `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateTransfer_UnexpectedError_Returns500(t *testing.T) {
	fake := &fakeTransferCreator{err: errors.New("boom")}
	h := NewTransferHandler(fake)

	rec := doRequest(t, h, `{"idempotencyKey":"k1","fromWalletId":"a","toWalletId":"b","amount":100}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
