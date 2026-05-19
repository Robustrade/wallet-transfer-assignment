package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

// TransferHandler serves the transfer endpoints.
type TransferHandler struct {
	api TransferAPI
}

type createRequest struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         json.Number `json:"amount"`
}

type transferResponse struct {
	ID             string      `json:"id"`
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         json.Number `json:"amount"`
	Status         string      `json:"status"`
	FailureReason  string      `json:"failureReason,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // decode amount as json.Number, never float64

	var req createRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	amount, err := decimal.NewFromString(req.Amount.String())
	if err != nil {
		writeError(w, http.StatusBadRequest, "amount must be a valid number")
		return
	}

	tr, created, err := h.api.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	status := http.StatusOK // duplicate idempotency key
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toResponse(tr))
}

// Get handles GET /transfers/{id}.
func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	tr, err := h.api.GetTransfer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(tr))
}

func toResponse(t domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         json.Number(t.Amount.String()),
		Status:         string(t.Status),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
	}
}

// writeServiceError maps domain sentinels to HTTP status codes. Insufficient
// funds never reaches here: it is a successful (FAILED) transfer, not an error.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrMissingField):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
