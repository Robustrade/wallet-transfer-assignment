package handler

import (
	"encoding/json"
	"net/http"

	"wallet-transfer/internal/transfer/dto"
	"wallet-transfer/internal/transfer/service"
	"wallet-transfer/pkg/errors"
	"wallet-transfer/pkg/logger"
)

// TransferHandler handles transfer HTTP requests
type TransferHandler struct {
	service *service.TransferService
	logger  *logger.Logger
}

// NewTransferHandler creates a new transfer handler
func NewTransferHandler(service *service.TransferService, logger *logger.Logger) *TransferHandler {
	return &TransferHandler{
		service: service,
		logger:  logger,
	}
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, errors.ErrInvalidRequest.WithDetails("reason", "invalid JSON"))
		return
	}

	resp, err := h.service.CreateTransfer(r.Context(), req)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.sendJSON(w, http.StatusCreated, resp)
}

// GetTransfer handles GET /transfers/{transferId}
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	transferID := r.PathValue("transferId")
	if transferID == "" {
		h.sendError(w, errors.ErrInvalidRequest.WithDetails("reason", "transferId is required"))
		return
	}

	resp, err := h.service.GetTransfer(r.Context(), transferID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.sendJSON(w, http.StatusOK, resp)
}

// handleServiceError maps service errors to HTTP responses
func (h *TransferHandler) handleServiceError(w http.ResponseWriter, err error) {
	customErr, ok := errors.IsCustomError(err)
	if !ok {
		h.logger.Error("unknown error type", map[string]interface{}{
			"error": err.Error(),
		})
		customErr = errors.ErrInternalError
	}

	h.sendError(w, customErr)
}

// sendJSON sends a JSON response
func (h *TransferHandler) sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// sendError sends an error response
func (h *TransferHandler) sendError(w http.ResponseWriter, err *errors.CustomError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.StatusCode)
	response := dto.ErrorResponse{
		Code:    err.Code,
		Message: err.Message,
		Details: err.Details,
	}
	json.NewEncoder(w).Encode(response)
}
