package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/models"
)

// TransferHandler exposes the transfer HTTP endpoints.
type TransferHandler struct {
	transfers TransferService
	logger    *slog.Logger
}

// NewTransferHandler constructs a TransferHandler.
func NewTransferHandler(transfers TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{transfers: transfers, logger: logger}
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, h.logger, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	result, err := h.transfers.CreateTransfer(r.Context(), req.ToInput())
	if err != nil {
		writeDomainError(w, h.logger, err)
		return
	}

	if result.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	WriteJSON(w, h.logger, result.Status, models.ToTransferResponse(result.Transfer, result.LedgerEntries))
}

// Get handles GET /transfers/{id}.
func (h *TransferHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		writeDomainError(w, h.logger, err)
		return
	}
	WriteJSON(w, h.logger, http.StatusOK, models.ToTransferResponse(result.Transfer, result.LedgerEntries))
}
