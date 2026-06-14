package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/service"
)

// TransferHandler exposes the transfer endpoints over HTTP.
// It is intentionally thin: decode → validate shape → call service → encode.
// Business rules live entirely in the service layer.
type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// transferRequest is the JSON body for POST /transfers.
type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResponse is the JSON body returned for every transfer, regardless of
// whether it was newly created or replayed from an idempotent duplicate.
type transferResponse struct {
	ID             uuid.UUID              `json:"id"`
	IdempotencyKey string                 `json:"idempotencyKey"`
	FromWalletID   string                 `json:"fromWalletId"`
	ToWalletID     string                 `json:"toWalletId"`
	Amount         int64                  `json:"amount"`
	Status         domain.TransferStatus  `json:"status"`
	FailureReason  string                 `json:"failureReason,omitempty"`
	CreatedAt      time.Time              `json:"createdAt"`
}

func toResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         t.Status,
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
	}
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req transferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	transfer, err := h.svc.CreateTransfer(r.Context(), service.TransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})

	if err != nil {
		status := httpStatusForError(err)
		// For idempotent replays or terminal failures we still return a
		// transfer body so the client can inspect the recorded outcome.
		if transfer != nil {
			writeJSON(w, status, toResponse(transfer))
			return
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toResponse(transfer))
}

// httpStatusForError maps domain sentinel errors to HTTP status codes.
func httpStatusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrAmountNotPositive),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrMissingWalletID),
		errors.Is(err, domain.ErrMissingIdempotency):
		return http.StatusBadRequest

	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound

	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity

	default:
		return http.StatusInternalServerError
	}
}
