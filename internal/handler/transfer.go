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
