
package handler

import (
    "encoding/json"
    "net/http"

    "wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
    service *service.TransferService
}

func NewTransferHandler(
    service *service.TransferService,
) *TransferHandler {

    return &TransferHandler{
        service: service,
    }
}

type CreateTransferRequest struct {
    IdempotencyKey string `json:"idempotencyKey"`
    FromWalletID   string `json:"fromWalletId"`
    ToWalletID     string `json:"toWalletId"`
    Amount         int64  `json:"amount"`
}

func (h *TransferHandler) CreateTransfer(
    w http.ResponseWriter,
    r *http.Request,
) {

    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    var req CreateTransferRequest

    err := json.NewDecoder(r.Body).Decode(&req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    transfer, err := h.service.CreateTransfer(
        req.IdempotencyKey,
        req.FromWalletID,
        req.ToWalletID,
        req.Amount,
    )

    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")

    json.NewEncoder(w).Encode(transfer)
}
