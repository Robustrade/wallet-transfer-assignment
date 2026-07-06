package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type WalletHandler struct {
	service *service.WalletService
}

func NewWalletHandler(service *service.WalletService) *WalletHandler {
	return &WalletHandler{service: service}
}

// GetBalance handles GET /v1/wallets/:id/balance
func (h *WalletHandler) GetBalance(c *gin.Context) {
	// Extract the :id parameter from the URL
	walletID := c.Param("id")
	if walletID == "" {
		RequestError(c, http.StatusBadRequest, "Wallet ID is required")
		return
	}

	// Call the service layer, passing the request context
	resp, err := h.service.GetWalletBalance(c.Request.Context(), walletID)
	if err != nil {
		if err == domain.ErrWalletNotFound {
			RequestError(c, http.StatusNotFound, "Wallet not found")
			return
		}

		RequestError(c, http.StatusInternalServerError, "Internal server error")
		return
	}

	// Return the successful payload
	RequestSuccess(c, http.StatusOK, resp)
}
