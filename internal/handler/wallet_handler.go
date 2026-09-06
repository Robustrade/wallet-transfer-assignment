package handler

import (
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type WalletHandler struct {
	walletService *service.WalletService
}

func NewWalletHandler(walletService *service.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

type CreateWalletInput struct {
	InitialBalance string `json:"initialBalance"`
	Currency       string `json:"currency"`
}

func (h *WalletHandler) CreateWallet(c *gin.Context) {
	var input CreateWalletInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, domain.ErrInvalidInput("invalid request body"))
		return
	}

	if input.InitialBalance == "" {
		input.InitialBalance = "0.00"
	}

	balance, err := domain.ParseMoney(input.InitialBalance)
	if err != nil {
		respondError(c, domain.ErrInvalidAmount(err.Error()))
		return
	}

	wallet, err := h.walletService.CreateWallet(c.Request.Context(), balance, input.Currency)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			respondError(c, appErr)
			return
		}
		respondError(c, domain.ErrInternal(err))
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"walletId":  wallet.ID.String(),
		"balance":   wallet.Balance.StringExact(),
		"currency":  wallet.Currency,
		"createdAt": wallet.CreatedAt,
		"updatedAt": wallet.UpdatedAt,
	})
}

func (h *WalletHandler) GetWalletBalance(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(c, domain.ErrInvalidInput("wallet id must be a valid UUID"))
		return
	}

	wallet, err := h.walletService.GetWalletBalance(c.Request.Context(), id)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			respondError(c, appErr)
			return
		}
		respondError(c, domain.ErrInternal(err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"walletId":  wallet.ID.String(),
		"balance":   wallet.Balance.StringExact(),
		"currency":  wallet.Currency,
		"updatedAt": wallet.UpdatedAt,
	})
}

func (h *WalletHandler) GetWalletTransactions(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(c, domain.ErrInvalidInput("wallet id must be a valid UUID"))
		return
	}

	entries, err := h.walletService.GetWalletTransactions(c.Request.Context(), id)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			respondError(c, appErr)
			return
		}
		respondError(c, domain.ErrInternal(err))
		return
	}

	type EntryResp struct {
		EntryID    string `json:"entryId"`
		TransferID string `json:"transferId"`
		Type       string `json:"type"`
		Amount     string `json:"amount"`
		CreatedAt  string `json:"createdAt"`
	}

	respEntries := make([]EntryResp, 0, len(entries))
	for _, e := range entries {
		respEntries = append(respEntries, EntryResp{
			EntryID:    e.ID.String(),
			TransferID: e.TransferID.String(),
			Type:       string(e.Type),
			Amount:     e.Amount.StringExact(),
			CreatedAt:  e.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"walletId": id.String(),
		"entries":  respEntries,
	})
}
