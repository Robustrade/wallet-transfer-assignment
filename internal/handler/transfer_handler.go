package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/service"
)

var uuidRegex = regexp.MustCompile(`^(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// TransferHandler handles HTTP requests for transfer operations.
// It is a thin layer responsible only for parsing requests, invoking the service,
// and mapping results/errors to HTTP responses.
type TransferHandler struct {
	service *service.TransferService
	logger  *slog.Logger
}

// NewTransferHandler creates a new TransferHandler.
func NewTransferHandler(svc *service.TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{service: svc, logger: logger}
}

// CreateTransfer handles POST /transfers.
// It parses the JSON request, delegates to the service, and returns the transfer result.
// Returns 201 Created for new transfers, 200 OK for idempotent replays.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON request body")
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), req)
	if err != nil {
		handleServiceError(w, h.logger, err)
		return
	}

	// Return 200 OK for idempotent replays, 201 Created for new transfers
	if result.IsDuplicate {
		writeJSON(w, http.StatusOK, result.Transfer)
	} else {
		writeJSON(w, http.StatusCreated, result.Transfer)
	}
}

// GetTransfer handles GET /transfers/{id}.
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !uuidRegex.MatchString(id) {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid transfer ID format; expected UUID")
		return
	}

	transfer, err := h.service.GetTransfer(r.Context(), id)
	if err != nil {
		handleServiceError(w, h.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, transfer)
}
