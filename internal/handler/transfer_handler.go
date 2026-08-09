package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
)

// TransferHandler handles HTTP endpoints for wallet transfers.
type TransferHandler struct {
	transferSvc *service.TransferService
}

// NewTransferHandler constructs a TransferHandler instance.
func NewTransferHandler(transferSvc *service.TransferService) *TransferHandler {
	return &TransferHandler{transferSvc: transferSvc}
}

// CreateTransferRequest defines the JSON payload for creating a transfer.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// CreateTransfer handles POST /transfers requests.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	result, err := h.transferSvc.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})

	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, domain.ErrInsufficientFunds) {
			if result != nil && result.Transfer != nil {
				writeJSONResponse(w, http.StatusBadRequest, result.Transfer)
				return
			}
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, domain.ErrInvalidAmount) || errors.Is(err, domain.ErrSameWalletTransfer) || errors.Is(err, domain.ErrInvalidWalletID) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			writeJSONError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, domain.ErrIdempotencyKeyReusedMismatch) {
			writeJSONError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to process transfer: "+err.Error())
		return
	}

	statusCode := http.StatusOK
	if !result.IsReplayed {
		statusCode = http.StatusCreated
	}

	writeJSONResponse(w, statusCode, result.Transfer)
}

// GetTransfer handles GET /transfers/{id} requests.
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	transferID := r.PathValue("id")
	if transferID == "" {
		transferID = r.URL.Query().Get("id")
	}
	if transferID == "" {
		writeJSONError(w, http.StatusBadRequest, "transfer id is required")
		return
	}

	transfer, ledger, err := h.transferSvc.GetTransferByID(r.Context(), transferID)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to retrieve transfer")
		return
	}

	resp := map[string]any{
		"transfer":      transfer,
		"ledgerEntries": ledger,
	}
	writeJSONResponse(w, http.StatusOK, resp)
}
