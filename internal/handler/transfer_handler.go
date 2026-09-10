package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/go-chi/chi/v5"
)

// TransferHandler handles HTTP requests for transfer operations.
type TransferHandler struct {
	transferService *service.TransferService
	logger          *slog.Logger
}

// NewTransferHandler creates a new TransferHandler.
func NewTransferHandler(transferService *service.TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
		logger:          logger,
	}
}

// RegisterRoutes registers transfer routes on the given router.
func (h *TransferHandler) RegisterRoutes(r chi.Router) {
	r.Post("/transfers", h.CreateTransfer)
	r.Get("/transfers/{id}", h.GetTransfer)
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if validationErr := req.Validate(); validationErr != nil {
		writeError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	result, err := h.transferService.CreateTransfer(
		r.Context(),
		req.IdempotencyKey,
		req.FromWalletID,
		req.ToWalletID,
		req.Amount,
	)

	if err != nil {
		h.handleTransferError(w, err)
		return
	}

	status := http.StatusCreated
	if result.IsDuplicate {
		status = http.StatusOK
	}

	writeJSON(w, status, toTransferResponse(result))
}

// GetTransfer handles GET /transfers/{id}
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "transfer ID is required")
		return
	}

	result, err := h.transferService.GetTransfer(r.Context(), id)
	if err != nil {
		h.handleTransferError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, toTransferResponse(result))
}

// handleTransferError maps domain errors to HTTP status codes.
func (h *TransferHandler) handleTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "insufficient funds")
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, "wallet not found")
	case errors.Is(err, domain.ErrTransferNotFound):
		writeError(w, http.StatusNotFound, "transfer not found")
	case errors.Is(err, domain.ErrSameWallet):
		writeError(w, http.StatusBadRequest, "source and destination wallets must be different")
	case errors.Is(err, domain.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, "amount must be positive")
	case errors.Is(err, domain.ErrEmptyIdempotencyKey):
		writeError(w, http.StatusBadRequest, "idempotency key is required")
	case errors.Is(err, domain.ErrEmptyWalletID):
		writeError(w, http.StatusBadRequest, "wallet ID is required")
	default:
		h.logger.ErrorContext(context.Background(), "unexpected error", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
