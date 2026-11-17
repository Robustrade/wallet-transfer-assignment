package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/skc1234/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	Service *service.TransferService
}

func NewTransferHandler(
	svc *service.TransferService,
) *TransferHandler {
	return &TransferHandler{
		Service: svc,
	}
}

func (h *TransferHandler) CreateTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Accept only POST requests.
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	// Read Idempotency-Key from request header.
	idempotencyKey := strings.TrimSpace(
		r.Header.Get("Idempotency-Key"),
	)

	if idempotencyKey == "" {
		http.Error(
			w,
			"Idempotency-Key header is required",
			http.StatusBadRequest,
		)
		return
	}

	// Request payload.
	var request struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}

	// Decode JSON request body.
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(
			w,
			"Invalid request body",
			http.StatusBadRequest,
		)
		return
	}

	// Call transfer service.
	transfer, err := h.Service.TransferMoney(
		r.Context(),
		idempotencyKey,
		request.FromWalletID,
		request.ToWalletID,
		request.Amount,
	)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)
		return
	}

	// Return successful transfer response.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(transfer); err != nil {
		return
	}
}
