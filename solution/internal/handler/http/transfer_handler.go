package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/service"
)

const maxBodyBytes = 64 << 10 // 64 KiB

// TransferCreator lets handler tests substitute a fake instead of *service.TransferService.
type TransferCreator interface {
	CreateTransfer(ctx context.Context, in service.CreateTransferInput) (*domain.Transfer, error)
}

// TransferHandler exposes the transfer HTTP API. It only does request
// validation/decoding and transport mapping — no business logic.
type TransferHandler struct {
	transfers TransferCreator
}

func NewTransferHandler(transfers TransferCreator) *TransferHandler {
	return &TransferHandler{transfers: transfers}
}

// POST /transfers
func (h *TransferHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Decode stops after the first JSON value, so without this a second object
	// or trailing junk would be ignored and the transfer would still execute.
	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "body must contain exactly one JSON object")
		return
	}

	if req.IdempotencyKey == "" || req.FromWalletID == "" || req.ToWalletID == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey, fromWalletId, and toWalletId are required")
		return
	}

	transfer, err := h.transfers.CreateTransfer(r.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, toTransferResponse(transfer))
}
