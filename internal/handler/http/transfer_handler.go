package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

// TransferCreator is the handler's view of the transfer service.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, cmd service.CreateTransferCommand) (service.TransferResult, error)
}

// TransferHandler serves the /transfers endpoints.
type TransferHandler struct {
	svc TransferCreator
	log *slog.Logger
}

// NewTransferHandler wires the handler.
func NewTransferHandler(svc TransferCreator, log *slog.Logger) *TransferHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &TransferHandler{svc: svc, log: log}
}

// Create handles POST /transfers.
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, h.log, fmt.Errorf("%w: malformed JSON body: %v", domain.ErrValidation, err))
		return
	}

	result, err := h.svc.CreateTransfer(r.Context(), req.toCommand())
	if err != nil {
		writeError(w, h.log, err)
		return
	}

	writeJSON(w, statusForResult(result), toTransferResponse(result.Transfer))
}

// statusForResult chooses the success status for a completed transfer.
func statusForResult(result service.TransferResult) int {
	switch result.Transfer.State {
	case domain.TransferProcessed:
		if result.Replayed {
			return http.StatusOK
		}
		return http.StatusCreated
	case domain.TransferFailed:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusAccepted
	}
}
