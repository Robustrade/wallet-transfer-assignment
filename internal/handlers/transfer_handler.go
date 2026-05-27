package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"wallet-transfer-service/internal/models"
	"wallet-transfer-service/internal/services"
)

// TransferHandler handles HTTP requests for transfers
type TransferHandler struct {
	transferService *services.TransferService
}

// NewTransferHandler creates a new transfer handler
func NewTransferHandler(transferService *services.TransferService) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
	}
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	// Parse request body
	var req models.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	// Call service layer
	response, err := h.transferService.CreateTransfer(&req)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	// Return success response
	h.respondJSON(w, http.StatusOK, response)
}

// handleServiceError maps service errors to HTTP responses
func (h *TransferHandler) handleServiceError(w http.ResponseWriter, err error) {
	switch e := err.(type) {
	case *models.ValidationError:
		h.respondError(w, http.StatusBadRequest, e.Message, e.Field)

	case *services.ConflictError:
		h.respondError(w, http.StatusConflict, e.Message, "")

	case *services.BusinessError:
		h.respondError(w, http.StatusBadRequest, e.Message, "")

	default:
		// Internal server error for unexpected errors
		log.Printf("Internal server error: %v", err)
		h.respondError(w, http.StatusInternalServerError, "internal server error", "")
	}
}

// respondJSON writes a JSON response
func (h *TransferHandler) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError writes an error response
func (h *TransferHandler) respondError(w http.ResponseWriter, status int, message string, field string) {
	errorResp := models.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Field:   field,
	}
	h.respondJSON(w, status, errorResp)
}
