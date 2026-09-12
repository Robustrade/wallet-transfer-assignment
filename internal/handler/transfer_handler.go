package handler

import (
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	transferService *service.TransferService
}

func NewTransferHandler(transferService *service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			WriteError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
			return
		}
	}

	var req service.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	resp, err := h.transferService.ExecuteTransfer(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInsufficientFunds):
			// Return 422 with the transfer failure details if available
			statusCode := http.StatusUnprocessableEntity
			if resp != nil && resp.ResponseCode > 0 {
				statusCode = resp.ResponseCode
			}
			if resp != nil {
				WriteJSON(w, statusCode, resp)
				return
			}
			WriteError(w, statusCode, err.Error())
			return

		case errors.Is(err, domain.ErrWalletNotFound):
			WriteError(w, http.StatusNotFound, err.Error())
			return

		case errors.Is(err, domain.ErrSystemTreasuryRestricted):
			WriteError(w, http.StatusForbidden, err.Error())
			return

		case errors.Is(err, domain.ErrSameWalletTransfer),
			errors.Is(err, domain.ErrInvalidAmount),
			errors.Is(err, domain.ErrMissingIdempotencyKey),
			errors.Is(err, domain.ErrInvalidIdempotencyKey),
			errors.Is(err, domain.ErrMissingWalletID),
			errors.Is(err, domain.ErrBalanceOverflow),
			errors.Is(err, domain.ErrCurrencyMismatch):
			WriteError(w, http.StatusBadRequest, err.Error())
			return

		case errors.Is(err, domain.ErrIdempotencyConflict):
			WriteError(w, http.StatusConflict, err.Error())
			return

		case errors.Is(err, domain.ErrIdempotencyInProgress):
			WriteError(w, http.StatusConflict, err.Error())
			return

		default:
			log.Printf("[ERROR] transfer execution failed: %v", err)
			WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	// Preserve the original status code (e.g. 201 Created) across both initial and replayed requests
	statusCode := http.StatusCreated
	if resp.ResponseCode > 0 {
		statusCode = resp.ResponseCode
	}
	WriteJSON(w, statusCode, resp)
}

// GetTransfer handles GET /transfers/{transfer_id}
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	transfer_id := r.PathValue("transfer_id")
	if transfer_id == "" {
		WriteError(w, http.StatusBadRequest, "transfer id is required")
		return
	}

	transfer, err := h.transferService.GetTransfer(r.Context(), transfer_id)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			WriteError(w, http.StatusNotFound, "transfer not found")
			return
		}
		log.Printf("[ERROR] failed to get transfer %s: %v", transfer_id, err)
		WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	WriteJSON(w, http.StatusOK, transfer)
}
