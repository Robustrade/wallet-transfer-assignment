package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/service"
)

// TransferHandler is a thin HTTP adapter: it only decodes the request,
// invokes the service, and maps the result/error to an HTTP response. It
// contains no business logic or persistence code.
type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failureReason,omitempty"`
}

func toTransferResponse(t domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		FailureReason:  t.FailureReason,
	}
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}

	transfer, err := h.service.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}

	status := http.StatusCreated
	if transfer.Status == domain.TransferStatusFailed {
		// The transfer request itself succeeded (it was accepted and
		// resolved); the business outcome failed. Report 200 with the
		// FAILED status in the body rather than a 4xx/5xx.
		status = http.StatusOK
	}
	writeJSON(w, status, toTransferResponse(transfer))
}

// Get handles GET /transfers/{id}.
func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	transfer, err := h.service.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, toTransferResponse(transfer))
}

// statusForError maps domain/service errors to HTTP status codes. This is
// the one piece of "business awareness" a handler is allowed: translating
// known error values to transport semantics, not deciding business rules.
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrMissingIdempotencyKey):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
