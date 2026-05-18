package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/candidate/wallet-transfer/internal/service"
	"github.com/candidate/wallet-transfer/internal/utils"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *service.TransferService
}

func New(svc *service.TransferService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	v1 := r.Group("/api/v1")

	wallets := v1.Group("/wallets")
	{
		wallets.POST("", h.CreateWallet)
		wallets.GET("/:id", h.GetWallet)
		wallets.GET("/:id/ledger", h.GetLedger)
	}

	transfers := v1.Group("/transfers")
	{
		transfers.POST("", h.CreateTransfer)
		transfers.GET("/:id", h.GetTransfer)
	}
}

type WalletResponse struct {
	Wallet *domain.Wallet `json:"wallet"`
}

type LedgerResponse struct {
	Entries    []*domain.LedgerEntry `json:"entries"`
	Limit      int                   `json:"limit"`
	NextCursor uint64                `json:"next_cursor"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// CreateWallet godoc
// @Summary      Create a new wallet
// @Description  Creates a new multi-currency wallet for a user initialized with 0 balance.
// @Tags         wallets
// @Accept       json
// @Produce      json
// @Param        request body service.CreateWalletRequest true "Wallet creation request"
// @Success      201 {object} WalletResponse
// @Failure      400 {object} ErrorResponse "Bad Request"
// @Failure      500 {object} ErrorResponse "Internal Server Error"
// @Router       /wallets [post]
func (h *Handler) CreateWallet(c *gin.Context) {
	var req service.CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorResponse(err.Error()))
		return
	}

	wallet, err := h.svc.CreateWallet(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.ErrorResponse(err.Error()))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"wallet": wallet})
}

// GetWallet godoc
// @Summary      Get wallet by ID
// @Description  Retrieves wallet details and current balance.
// @Tags         wallets
// @Produce      json
// @Param        id path int true "Wallet ID"
// @Success      200 {object} WalletResponse
// @Failure      400 {object} ErrorResponse "Bad Request"
// @Failure      404 {object} ErrorResponse "Not Found"
// @Failure      500 {object} ErrorResponse "Internal Server Error"
// @Router       /wallets/{id} [get]
func (h *Handler) GetWallet(c *gin.Context) {
	id, err := utils.ParseUint(c, "id")
	if err != nil {
		return
	}

	wallet, err := h.svc.GetWallet(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			c.JSON(http.StatusNotFound, utils.ErrorResponse("wallet not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, utils.ErrorResponse(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"wallet": wallet})
}

// GetLedger godoc
// @Summary      Get wallet ledger entries
// @Description  Retrieves paginated double-entry ledger history for a wallet using cursor pagination.
// @Tags         wallets
// @Produce      json
// @Param        id path int true "Wallet ID"
// @Param        limit query int false "Page limit (max 100)" default(20)
// @Param        cursor query int false "Pagination cursor (entry ID)"
// @Success      200 {object} LedgerResponse
// @Failure      400 {object} ErrorResponse "Bad Request"
// @Failure      500 {object} ErrorResponse "Internal Server Error"
// @Router       /wallets/{id}/ledger [get]
func (h *Handler) GetLedger(c *gin.Context) {
	id, err := utils.ParseUint(c, "id")
	if err != nil {
		return
	}

	limit := utils.QueryInt(c, "limit", 20)
	var cursor uint64
	if cursorStr := c.Query("cursor"); cursorStr != "" {
		parsed, err := strconv.ParseUint(cursorStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, utils.ErrorResponse("invalid cursor value"))
			return
		}
		cursor = parsed
	}
	
	if limit > 100 {
		limit = 100
	}

	entries, err := h.svc.GetLedger(c.Request.Context(), id, limit, cursor)
	if err != nil {
		c.JSON(http.StatusInternalServerError, utils.ErrorResponse(err.Error()))
		return
	}
	
	var nextCursor uint64
	if len(entries) > 0 {
		nextCursor = entries[len(entries)-1].ID
	}
	
	c.JSON(http.StatusOK, gin.H{"entries": entries, "limit": limit, "next_cursor": nextCursor})
}

// CreateTransfer godoc
// @Summary      Create a wallet transfer
// @Description  Performs an atomic, idempotent transfer between two wallets with exactly-once guarantees.
// @Tags         transfers
// @Accept       json
// @Produce      json
// @Param        request body service.TransferRequest true "Transfer request payload"
// @Success      201 {object} service.TransferResponse
// @Failure      400 {object} ErrorResponse "Bad Request (Same wallet, invalid amount, currency mismatch)"
// @Failure      404 {object} ErrorResponse "Wallet Not Found"
// @Failure      409 {object} ErrorResponse "Idempotency Conflict"
// @Failure      422 {object} ErrorResponse "Insufficient Funds"
// @Failure      500 {object} ErrorResponse "Internal Server Error"
// @Router       /transfers [post]
func (h *Handler) CreateTransfer(c *gin.Context) {
	var req service.TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, utils.ErrorResponse(err.Error()))
		return
	}

	resp, err := h.svc.Transfer(c.Request.Context(), &req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInsufficientFunds):
			c.JSON(http.StatusUnprocessableEntity, utils.ErrorResponse(err.Error()))
		case errors.Is(err, domain.ErrIdempotencyConflict):
			c.JSON(http.StatusConflict, utils.ErrorResponse(err.Error()))
		case errors.Is(err, domain.ErrSameWallet),
			errors.Is(err, domain.ErrInvalidAmount),
			errors.Is(err, domain.ErrCurrencyMismatch):
			c.JSON(http.StatusBadRequest, utils.ErrorResponse(err.Error()))
		case errors.Is(err, domain.ErrWalletNotFound):
			c.JSON(http.StatusNotFound, utils.ErrorResponse(err.Error()))
		default:
			c.JSON(http.StatusInternalServerError, utils.ErrorResponse(err.Error()))
		}
		return
	}

	status := http.StatusCreated
	c.JSON(status, resp)
}

// GetTransfer godoc
// @Summary      Get transfer transaction by ID
// @Description  Retrieves transaction details, status, and metadata.
// @Tags         transfers
// @Produce      json
// @Param        id path int true "Transaction ID"
// @Success      200 {object} service.TransferResponse
// @Failure      400 {object} ErrorResponse "Bad Request"
// @Failure      404 {object} ErrorResponse "Not Found"
// @Failure      500 {object} ErrorResponse "Internal Server Error"
// @Router       /transfers/{id} [get]
func (h *Handler) GetTransfer(c *gin.Context) {
	id, err := utils.ParseUint(c, "id")
	if err != nil {
		return
	}

	txn, err := h.svc.GetTransaction(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrTransactionNotFound) {
			c.JSON(http.StatusNotFound, utils.ErrorResponse("transaction not found"))
			return
		}
		c.JSON(http.StatusInternalServerError, utils.ErrorResponse(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"transaction": txn})
}
