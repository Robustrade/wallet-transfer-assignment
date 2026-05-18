package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/service"
)

// TransferHandler handles HTTP requests for the transfer API.
type TransferHandler struct {
	service *service.TransferService
	logger  *slog.Logger
}

// NewTransferHandler creates a new TransferHandler.
func NewTransferHandler(svc *service.TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{
		service: svc,
		logger:  logger,
	}
}

// HandleCreateTransfer handles POST /transfers requests.
func (h *TransferHandler) HandleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req domain.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON body",
		})
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), &req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(result.StatusCode)
	w.Write(result.Body)
}

// handleError maps domain errors to HTTP responses.
func (h *TransferHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest):
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInvalidAmount):
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrSameWallet):
		h.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrWalletNotFound):
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, domain.ErrInsufficientBalance):
		h.writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	default:
		h.logger.Error("handler.internal_error", "error", err.Error())
		h.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
}

// writeJSON is a helper to write a JSON response.
func (h *TransferHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
