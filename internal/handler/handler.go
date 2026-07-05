// Package handler maps HTTP requests to service calls. Handlers stay thin:
// request validation and transport mapping only, no business logic.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/service"
)

// transferService is the subset of *service.TransferService the handler
// depends on. Declaring it lets tests substitute a fake without hitting a
// real database.
type transferService interface {
	CreateTransfer(ctx context.Context, req service.CreateTransferRequest) (domain.Transfer, error)
}

// TransferHandler serves the /transfers endpoints.
type TransferHandler struct {
	service transferService
}

// NewTransferHandler constructs a TransferHandler around a TransferService.
func NewTransferHandler(svc transferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

type createTransferRequestDTO struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponseDTO struct {
	TransferID    string  `json:"transferId"`
	Status        string  `json:"status"`
	FromWalletID  string  `json:"fromWalletId"`
	ToWalletID    string  `json:"toWalletId"`
	Amount        int64   `json:"amount"`
	FailureReason *string `json:"failureReason"`
	CreatedAt     string  `json:"createdAt"`
}

type errorResponseDTO struct {
	Error string `json:"error"`
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, domain.ErrIdempotencyKeyRequired.Error())
		return
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		writeError(w, http.StatusBadRequest, domain.ErrWalletIdRequired.Error())
		return
	}
	if req.FromWalletID == req.ToWalletID {
		writeError(w, http.StatusBadRequest, domain.ErrSameWallet.Error())
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, domain.ErrInvalidAmount.Error())
		return
	}
	transfer, err := h.service.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, transferResponseDTO{
		TransferID:    transfer.ID.String(),
		Status:        string(transfer.Status),
		FromWalletID:  transfer.FromWalletID,
		ToWalletID:    transfer.ToWalletID,
		Amount:        transfer.Amount,
		FailureReason: transfer.FailureReason,
		CreatedAt:     transfer.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
	})
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrWalletIdRequired),
		errors.Is(err, domain.ErrIdempotencyKeyRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponseDTO{Error: message})
}
