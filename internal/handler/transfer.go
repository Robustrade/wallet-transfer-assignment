package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	svc    *service.TransferService
	logger *slog.Logger
}

func NewTransferHandler(svc *service.TransferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{svc: svc, logger: logger}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// CreateTransfer godoc
// @Summary      Create a wallet transfer
// @Description  Creates an idempotent wallet-to-wallet transfer. Replaying the same idempotencyKey returns the original result (HTTP 200).
// @Tags         transfers
// @Accept       json
// @Produce      json
// @Param        request body CreateTransferRequest true "Transfer request"
// @Success      201 {object} TransferResponse "Transfer created"
// @Success      200 {object} TransferResponse "Idempotent replay"
// @Failure      400 {object} ErrorResponse
// @Failure      404 {object} ErrorResponse
// @Failure      422 {object} TransferResponse "Insufficient funds (FAILED status)"
// @Failure      500 {object} ErrorResponse
// @Router       /transfers [post]
func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req createTransferRequest
	if err := bindJSON(c, &req); err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := h.svc.CreateTransfer(c.Request.Context(), domain.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeDomainError(c, err)
		return
	}

	status := http.StatusCreated
	if result.IsReplay {
		status = http.StatusOK
	} else if result.Status == domain.StatusFailed {
		switch result.FailureReason {
		case domain.ErrWalletNotFound.Error():
			status = http.StatusNotFound
		default:
			status = http.StatusUnprocessableEntity
		}
	}

	writeJSON(c, status, result)
}

type HealthHandler struct {
	pinger Pinger
}

type Pinger interface {
	Ping(c *gin.Context) error
}

func NewHealthHandler(pinger Pinger) *HealthHandler {
	return &HealthHandler{pinger: pinger}
}

// Health godoc
// @Summary      Health check
// @Description  Returns service health; verifies database connectivity.
// @Tags         ops
// @Produce      json
// @Success      200 {object} HealthResponse
// @Failure      503 {object} HealthResponse
// @Router       /health [get]
func (h *HealthHandler) Health(c *gin.Context) {
	if err := h.pinger.Ping(c); err != nil {
		writeJSON(c, http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(c, http.StatusOK, gin.H{"status": "ok"})
}

type WalletHandler struct {
	balances WalletBalanceReader
}

type WalletBalanceReader interface {
	GetWalletBalance(c *gin.Context, walletID string) (int64, error)
}

func NewWalletHandler(reader WalletBalanceReader) *WalletHandler {
	return &WalletHandler{balances: reader}
}

// GetBalance godoc
// @Summary      Get wallet balance
// @Description  Returns the current stored balance for a wallet.
// @Tags         wallets
// @Produce      json
// @Param        id path string true "Wallet ID" example(wallet_1)
// @Success      200 {object} WalletBalanceResponse
// @Failure      404 {object} ErrorResponse
// @Failure      500 {object} ErrorResponse
// @Router       /wallets/{id}/balance [get]
func (h *WalletHandler) GetBalance(c *gin.Context) {
	walletID := c.Param("id")
	balance, err := h.balances.GetWalletBalance(c, walletID)
	if err != nil {
		writeDomainError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, gin.H{
		"walletId": walletID,
		"balance":  balance,
	})
}
