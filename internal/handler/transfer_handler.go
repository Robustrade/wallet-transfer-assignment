package handler

import (
	"errors"
	"net/http"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TransferHandler struct {
	transferService service.TransferService
	walletRepo      repository.WalletRepository
	db              *gorm.DB
}

func NewTransferHandler(transferService service.TransferService, walletRepo repository.WalletRepository, db *gorm.DB) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
		walletRepo:      walletRepo,
		db:              db,
	}
}

func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req service.ProcessTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body: " + err.Error()})
		return
	}

	if req.IdempotencyKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'idempotencyKey'"})
		return
	}
	if req.FromWalletID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'fromWalletId'"})
		return
	}
	if req.ToWalletID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'toWalletId'"})
		return
	}
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Amount must be greater than zero"})
		return
	}

	resp, statusCode, err := h.transferService.ProcessTransfer(c.Request.Context(), &req)
	if err != nil {
		if errors.Is(err, domain.ErrConcurrentRequest) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(statusCode, resp)
}

func (h *TransferHandler) GetWallet(c *gin.Context) {
	walletID := c.Param("id")
	if walletID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing wallet ID"})
		return
	}

	wallet, err := h.walletRepo.Get(c.Request.Context(), h.db, walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Wallet not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, wallet)
}

type CreateWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

func (h *TransferHandler) CreateWallet(c *gin.Context) {
	var req CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body: " + err.Error()})
		return
	}

	if req.ID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing 'id'"})
		return
	}
	if req.InitialBalance < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Initial balance cannot be negative"})
		return
	}

	wallet := &domain.Wallet{
		ID:      req.ID,
		Balance: req.InitialBalance,
	}

	err := h.walletRepo.Create(c.Request.Context(), h.db, wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, wallet)
}
