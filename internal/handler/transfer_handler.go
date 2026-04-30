package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"github.com/gerry1818/wallet-transfer-assignment/internal/model/dto"
	"github.com/gerry1818/wallet-transfer-assignment/internal/service"
	"go.uber.org/zap"
)

type Handler struct {
	svc *service.TransferService
}

func NewHandler(s *service.TransferService) *Handler {
	return &Handler{svc: s}
}

func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(dto.NewErrorResponse("method not allowed", "METHOD_NOT_ALLOWED"))
		return
	}

	var reqDTO dto.TransferRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&reqDTO); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(dto.NewErrorResponse("invalid request body", "INVALID_REQUEST"))
		return
	}

	// Create request context for structured logging
	reqCtx := logger.NewRequestContext(reqDTO.IdempotencyKey)

	// Create timeout context
	ctx, cancel := NewContextWithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Convert DTO to internal model
	req := model.TransferRequest{
		IdempotencyKey: reqDTO.IdempotencyKey,
		FromWalletID:   reqDTO.FromWalletID,
		ToWalletID:     reqDTO.ToWalletID,
		Amount:         reqDTO.Amount,
	}

	reqCtx.Info("transfer request received")

	resp, code, err := h.svc.Transfer(ctx, req)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	if err != nil {
		reqCtx.Error("transfer failed", err)
		json.NewEncoder(w).Encode(dto.NewErrorResponse(err.Error(), "TRANSFER_FAILED"))
		return
	}

	if resp != nil {
		reqCtx = reqCtx.WithTransferID(resp.TransferID)
		reqCtx.Info("transfer successful", zap.String("status", resp.Status))

		respDTO := dto.TransferResponseDTO{
			TransferID: resp.TransferID,
			Status:     resp.Status,
		}
		json.NewEncoder(w).Encode(respDTO)
	}
}
