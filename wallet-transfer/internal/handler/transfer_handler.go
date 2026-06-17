// Package handler is the HTTP transport layer. It decodes and structurally
// validates requests, invokes the service, and maps results and typed domain
// errors to HTTP status codes. It contains no business logic and no SQL.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/service"
)

type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// Routes registers the handler on a ServeMux (Go 1.22 method+path patterns).
func (h *TransferHandler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /transfers", h.createTransfer)
}

type createTransferRequest struct {
	IdempotencyKey string        `json:"idempotencyKey"`
	FromWalletID   string        `json:"fromWalletId"`
	ToWalletID     string        `json:"toWalletId"`
	Amount         domain.Amount `json:"amount"`
}

func (h *TransferHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed JSON body")
		return
	}

	// Structural validation -- the service never sees a malformed command.
	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey is required")
		return
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		writeError(w, http.StatusBadRequest, "fromWalletId and toWalletId are required")
		return
	}
	if req.FromWalletID == req.ToWalletID {
		writeError(w, http.StatusBadRequest, "fromWalletId and toWalletId must differ")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount must be a positive integer (minor units)")
		return
	}

	res, err := h.svc.CreateTransfer(r.Context(), service.Command{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Success or recorded FAILED -- the service decided the status and body.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.HTTPStatus)
	_, _ = w.Write(res.Body)
}

// writeServiceError maps typed domain errors to HTTP statuses.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "wallet not found")
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusUnprocessableEntity, "idempotency key reused with a different payload")
	case errors.Is(err, service.ErrProcessing):
		writeError(w, http.StatusConflict, "a request with this idempotency key is still processing")
	default:
		// Infrastructure / unexpected: the transaction rolled back.
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
