package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/service"
)

func TestCreateTransferHandler(t *testing.T) {
	useCase := &stubTransferUseCase{
		result: &service.TransferResult{Transfer: &domain.Transfer{
			ID:             "transfer-1",
			IdempotencyKey: "key-1",
			FromWalletID:   "wallet-1",
			ToWalletID:     "wallet-2",
			Amount:         100,
			Status:         domain.TransferStatusProcessed,
			CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
		}},
	}
	handler := NewHandler(useCase, discardLogger()).Routes()
	body := bytes.NewBufferString(`{
		"idempotencyKey":"key-1",
		"fromWalletId":"wallet-1",
		"toWalletId":"wallet-2",
		"amount":100
	}`)
	request := httptest.NewRequest(http.MethodPost, "/transfers", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var response transferResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.TransferID != "transfer-1" || response.Status != domain.TransferStatusProcessed {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestCreateTransferHandlerMapsIdempotencyConflict(t *testing.T) {
	useCase := &stubTransferUseCase{err: domain.ErrIdempotencyConflict}
	handler := NewHandler(useCase, discardLogger()).Routes()
	request := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(`{
			"idempotencyKey":"key-1",
			"fromWalletId":"wallet-1",
			"toWalletId":"wallet-2",
			"amount":100
		}`),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestCreateTransferHandlerRejectsUnknownFields(t *testing.T) {
	handler := NewHandler(&stubTransferUseCase{}, discardLogger()).Routes()
	request := httptest.NewRequest(
		http.MethodPost,
		"/transfers",
		bytes.NewBufferString(`{"idempotencyKey":"key-1","unexpected":true}`),
	)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

type stubTransferUseCase struct {
	result *service.TransferResult
	err    error
}

func (s *stubTransferUseCase) CreateTransfer(
	context.Context,
	service.TransferCommand,
) (*service.TransferResult, error) {
	return s.result, s.err
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
