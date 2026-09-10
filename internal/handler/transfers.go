package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/helper"
)

type TransferService interface {
	CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.CreateTransferResult, error)
}

type TransferHandler struct {
	service TransferService
}

func NewTransferHandler(service TransferService) *TransferHandler {
	return &TransferHandler{
		service: service,
	}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var request domain.CreateTransferRequest

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil {
		helper.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), request)
	if err != nil {
		helper.HandleError(w, result, err)
		return
	}

	helper.WriteJSON(w, http.StatusCreated, result)
}
