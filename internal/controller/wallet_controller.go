package controller

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/response"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type createWalletRequest struct {
	WalletID       string `json:"walletId"`
	InitialBalance *int64 `json:"initialBalance"`
}

type walletResponse struct {
	WalletID  string    `json:"walletId"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{
		WalletID:  w.ID,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}

func CreateWallet(c *gin.Context) {
	var req createWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "", "request body must be valid JSON with the expected field types")
		return
	}
	if req.InitialBalance == nil {
		response.BadRequest(c, "initialBalance", "is required")
		return
	}

	svc := service.WalletService{}
	wallet, err := svc.CreateWallet(c.Request.Context(), req.WalletID, *req.InitialBalance)
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, toWalletResponse(wallet))
}
