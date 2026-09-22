package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

func TestWriteDomainErrorMapping(t *testing.T) {
	wrappedWallet := errors.New("wrapped wallet")
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "wallet not found", err: domain.WalletNotFoundError{ID: "missing"}, status: http.StatusNotFound, code: "wallet_not_found"},
		{name: "transfer not found", err: domain.ErrTransferNotFound, status: http.StatusNotFound, code: "transfer_not_found"},
		{name: "wallet already exists", err: domain.ErrWalletAlreadyExists, status: http.StatusConflict, code: "wallet_already_exists"},
		{name: "invalid request", err: domain.ErrInvalidAmount, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "idempotency conflict", err: domain.ErrIdempotencyKeyConflict, status: http.StatusConflict, code: "idempotency_key_conflict"},
		{name: "idempotency in progress", err: domain.ErrIdempotencyInProgress, status: http.StatusConflict, code: "idempotency_in_progress"},
		{name: "unknown error", err: wrappedWallet, status: http.StatusInternalServerError, code: "internal_error"},
	}

	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeDomainError(response, logger, tt.err)

			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d", response.Code, tt.status)
			}
			var body struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Code != tt.code {
				t.Errorf("code = %q, want %q", body.Code, tt.code)
			}
		})
	}
}

func TestWriteJSONNilValue(t *testing.T) {
	response := httptest.NewRecorder()
	WriteJSON(response, slog.Default(), http.StatusNoContent, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
