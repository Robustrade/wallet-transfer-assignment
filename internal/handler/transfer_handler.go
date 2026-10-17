package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// TransferHandler handles HTTP endpoints for transfers.
type TransferHandler struct {
	transferService service.TransferService
}

// NewTransferHandler constructs a new TransferHandler.
func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

// HandleCreateTransfer processes POST /transfers
func (h *TransferHandler) HandleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		WriteJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: ErrorDetail{
			Code: domain.CodeInvalidRequest, Message: "method not allowed",
		}})
		return
	}

	// Limit request body size to prevent memory exhaustion attacks
	r.Body = http.MaxBytesReader(w, r.Body, 1024*64) // 64 KB max

	var req service.CreateTransferRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Reject unexpected or malformed payload fields

	if err := decoder.Decode(&req); err != nil {
		message := "malformed JSON request body or unknown fields present"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			message = "request body must not exceed 64 KB"
		}
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, message))
		return
	}

	// Ensure there is no trailing garbage after JSON
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, "request body must contain a single JSON object"))
		return
	}

	// Trim whitespace
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.FromWalletID = strings.TrimSpace(req.FromWalletID)
	req.ToWalletID = strings.TrimSpace(req.ToWalletID)

	result, err := h.transferService.CreateTransfer(r.Context(), &req)
	if err != nil {
		WriteError(w, err)
		return
	}

	// Write response headers and original / saved body
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if result.Replayed {
		w.Header().Set("X-Idempotent-Replayed", "true")
	}
	w.WriteHeader(result.StatusCode)
	_, _ = w.Write(result.ResponseBody)
}

// HandleGetTransfer processes GET /transfers/{id}
func (h *TransferHandler) HandleGetTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, "method not allowed"))
		return
	}

	transferID := r.PathValue("id")
	if transferID == "" {
		transferID = strings.TrimPrefix(r.URL.Path, "/transfers/")
	}
	transferID = strings.TrimSpace(transferID)
	if transferID == "" {
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, "transfer id is required"))
		return
	}

	transfer, err := h.transferService.GetTransfer(r.Context(), transferID)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, transfer)
}
