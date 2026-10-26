// Package handler provides the transfer API's HTTP boundary. All financial
// decisions and PostgreSQL operations remain in the service and repository.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"wallettransfer/internal/domain"
)

const maxRequestBodyBytes = 8 << 10

// Transferer is satisfied by *service.Service. The handler depends only on
// this behavior, not on the repository or database.
type Transferer interface {
	Transfer(context.Context, domain.TransferRequest) (domain.TransferResult, error)
}

type handler struct{ transferer Transferer }

type transferRequestJSON struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponseJSON struct {
	TransferID    string `json:"transferId"`
	Status        string `json:"status"`
	FailureReason string `json:"failureReason,omitempty"`
}

type errorResponseJSON struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// New returns a standard-library HTTP handler exposing POST /transfers.
// A standard ServeMux supplies 404 and 405 responses for other routes/methods.
func New(transferer Transferer) http.Handler {
	h := &handler{transferer: transferer}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.createTransfer)
	return mux
}

func (h *handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	// MaxBytesReader also covers chunked requests without Content-Length.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var payload transferRequestJSON
	if err := dec.Decode(&payload); err != nil {
		writeDecodeError(w, err)
		return
	}
	// Reject a second JSON value (including trailing malformed input).
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "Only one JSON object is allowed")
		} else {
			writeDecodeError(w, err)
		}
		return
	}

	request := domain.TransferRequest{
		IdempotencyKey: payload.IdempotencyKey,
		FromWalletID:   payload.FromWalletID,
		ToWalletID:     payload.ToWalletID,
		Amount:         payload.Amount,
	}
	result, err := h.transferer.Transfer(r.Context(), request)
	if err != nil {
		writeTransferError(w, err)
		return
	}

	status := http.StatusCreated
	if result.Status == domain.StatusFailed {
		// A confirmed business failure is durably stored and replayed as 409.
		status = http.StatusConflict
	}
	writeJSON(w, status, transferResponseJSON{
		TransferID:    result.ID,
		Status:        result.Status,
		FailureReason: result.FailureReason,
	})
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "JSON body exceeds 8 KiB")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "Expected one valid JSON object with the documented fields")
}

func writeTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidWalletID),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "wallet_not_found", err.Error())
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled),
		errors.Is(err, domain.ErrRetryableTransaction):
		writeError(w, http.StatusServiceUnavailable, "outcome_unknown", "Transfer outcome may be unknown; retry with the same idempotency key")
	default:
		// Do not expose SQL errors, connection strings, or financial data.
		slog.Error("transfer request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "outcome_unknown", "Transfer outcome may be unknown; retry with the same idempotency key")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponseJSON{Error: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// All response values are simple structs with safe primitive fields;
	// encoding errors should not occur for these types.
	_ = json.NewEncoder(w).Encode(value)
}
