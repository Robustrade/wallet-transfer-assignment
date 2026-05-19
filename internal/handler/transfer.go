package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/service"
)

// TransferHandler handles transfer HTTP endpoints.
type TransferHandler struct {
	transfers service.TransferService
}

// NewTransferHandler creates a TransferHandler.
func NewTransferHandler(transfers service.TransferService) *TransferHandler {
	return &TransferHandler{transfers: transfers}
}

// createTransferRequest is the JSON body for POST /transfers.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// createTransferResponse is the JSON body returned from POST /transfers.
type createTransferResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency,omitempty"`
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if err := validateCreateTransferRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.transfers.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createTransferResponse{
		TransferID: result.TransferID,
		State:      string(result.State),
		Amount:     result.Amount,
		Currency:   result.Currency,
	})
}

func validateCreateTransferRequest(req createTransferRequest) error {
	if req.IdempotencyKey == "" {
		return errors.New("idempotencyKey is required")
	}
	if req.FromWalletID == "" {
		return errors.New("fromWalletId is required")
	}
	if req.ToWalletID == "" {
		return errors.New("toWalletId is required")
	}
	if req.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	if err := domain.ValidateTransferRequest(req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return err
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotImplemented):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrSameWallet), errors.Is(err, domain.ErrInvalidTransfer):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
