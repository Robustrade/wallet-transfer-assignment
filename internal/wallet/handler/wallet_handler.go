package handler

import (
	"encoding/json"
	"net/http"

	"wallet-transfer/internal/wallet/dto"
	"wallet-transfer/internal/wallet/service"
	"wallet-transfer/pkg/errors"
	"wallet-transfer/pkg/logger"
)

// WalletHandler handles wallet HTTP requests
type WalletHandler struct {
	service *service.WalletService
	logger  *logger.Logger
}

// NewWalletHandler creates a new wallet handler
func NewWalletHandler(svc *service.WalletService, logger *logger.Logger) *WalletHandler {
	return &WalletHandler{
		service: svc,
		logger:  logger,
	}
}

// GetWallet handles GET /wallets/{walletId}
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("walletId")
	if walletID == "" {
		h.sendError(w, errors.ErrInvalidRequest.WithDetails("reason", "walletId is required"))
		return
	}

	walletDetails, err := h.service.GetWallet(r.Context(), walletID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	resp := dto.GetWalletResponse{
		WalletID:  walletDetails.WalletID,
		Balance:   walletDetails.Balance,
		Currency:  walletDetails.Currency,
		CreatedAt: walletDetails.CreatedAt,
		UpdatedAt: walletDetails.UpdatedAt,
	}

	h.sendJSON(w, http.StatusOK, resp)
}

// GetBalance handles GET /wallets/{walletId}/balance
func (h *WalletHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("walletId")
	if walletID == "" {
		h.sendError(w, errors.ErrInvalidRequest.WithDetails("reason", "walletId is required"))
		return
	}

	balance, err := h.service.GetBalance(r.Context(), walletID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	h.sendJSON(w, http.StatusOK, map[string]interface{}{
		"walletId": walletID,
		"balance":  balance,
	})
}

// handleServiceError maps service errors to HTTP responses
func (h *WalletHandler) handleServiceError(w http.ResponseWriter, err error) {
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
func (h *WalletHandler) sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// sendError sends an error response
func (h *WalletHandler) sendError(w http.ResponseWriter, err *errors.CustomError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.StatusCode)
	response := map[string]interface{}{
		"code":    err.Code,
		"message": err.Message,
	}
	if len(err.Details) > 0 {
		response["details"] = err.Details
	}
	json.NewEncoder(w).Encode(response)
}
