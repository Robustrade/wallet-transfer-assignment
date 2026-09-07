package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/model"
	"wallet-transfer-service/internal/service"
)

type WalletHandler struct {
	walletService service.WalletService
}

func NewWalletHandler(walletService service.WalletService) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
	}
}

// CreateWallet handles POST /api/v1/wallets
func (h *WalletHandler) CreateWallet(c *gin.Context) {
	var req model.CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.WarnContext(c.Request.Context(), "create wallet request validation failed", "error", err)
		HandleValidationError(c, err)
		return
	}
	slog.DebugContext(c.Request.Context(), "create wallet request received", "wallet_id", req.ID)

	wallet, statusCode, err := h.walletService.CreateWallet(c.Request.Context(), &req)
	if err != nil {
		status := statusCode
		if status == 0 {
			status = http.StatusInternalServerError
		}

		switch {
		case errors.Is(err, apperror.ErrInvalidInitialBalance),
			errors.Is(err, apperror.ErrInvalidAmount),
			errors.Is(err, apperror.ErrWalletIDRequired):
			c.JSON(status, gin.H{"error": err.Error()})
		case errors.Is(err, apperror.ErrWalletAlreadyExists):
			c.JSON(status, gin.H{"error": "wallet already exists"})
		default:
			c.JSON(status, gin.H{"error": "failed to create wallet"})
		}
		slog.WarnContext(c.Request.Context(), "create wallet request failed", "wallet_id", req.ID, "status", status, "error", err)
		return
	}

	if statusCode == 0 {
		statusCode = http.StatusCreated
	}

	c.JSON(statusCode, model.WalletResponse{
		ID:      wallet.ID,
		Balance: model.FormatMinorUnits(wallet.Balance),
		Status:  wallet.Status,
	})
	slog.InfoContext(c.Request.Context(), "create wallet request completed", "wallet_id", wallet.ID, "status", statusCode)
}

// GetWallet handles GET /api/v1/wallets/:id
func (h *WalletHandler) GetWallet(c *gin.Context) {
	var uriParam model.WalletURIParam
	if err := c.ShouldBindUri(&uriParam); err != nil {
		slog.WarnContext(c.Request.Context(), "get wallet request validation failed", "error", err)
		HandleValidationError(c, err)
		return
	}

	wallet, statusCode, err := h.walletService.GetWallet(c.Request.Context(), uriParam.ID)
	if err != nil {
		status := statusCode
		if status == 0 {
			status = http.StatusInternalServerError
		}

		if errors.Is(err, apperror.ErrWalletNotFound) {
			c.JSON(status, gin.H{"error": "wallet not found"})
			slog.WarnContext(c.Request.Context(), "get wallet request failed: wallet not found", "wallet_id", uriParam.ID, "status", status)
			return
		}
		c.JSON(status, gin.H{"error": "failed to fetch wallet"})
		slog.ErrorContext(c.Request.Context(), "get wallet request failed", "wallet_id", uriParam.ID, "status", status, "error", err)
		return
	}

	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	c.JSON(statusCode, model.WalletResponse{
		ID:      wallet.ID,
		Balance: model.FormatMinorUnits(wallet.Balance),
		Status:  wallet.Status,
	})
	slog.InfoContext(c.Request.Context(), "get wallet request completed", "wallet_id", wallet.ID, "status", statusCode)
}

// UpdateStatus handles PATCH /api/v1/wallets/:id/status
func (h *WalletHandler) UpdateStatus(c *gin.Context) {
	var uriParam model.WalletURIParam
	if err := c.ShouldBindUri(&uriParam); err != nil {
		slog.WarnContext(c.Request.Context(), "update wallet status path validation failed", "error", err)
		HandleValidationError(c, err)
		return
	}

	var req model.UpdateWalletStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.WarnContext(c.Request.Context(), "update wallet status request validation failed", "wallet_id", uriParam.ID, "error", err)
		HandleValidationError(c, err)
		return
	}

	statusCode, err := h.walletService.UpdateWalletStatus(
		c.Request.Context(),
		uriParam.ID,
		model.WalletStatus(req.Status),
	)
	if err != nil {
		status := statusCode
		if status == 0 {
			status = http.StatusInternalServerError
		}

		if errors.Is(err, apperror.ErrWalletNotFound) {
			c.JSON(status, gin.H{"error": "wallet not found"})
			slog.WarnContext(c.Request.Context(), "update wallet status failed: wallet not found", "wallet_id", uriParam.ID, "status", status)
			return
		}
		c.JSON(status, gin.H{"error": "failed to update wallet status"})
		slog.ErrorContext(c.Request.Context(), "update wallet status failed", "wallet_id", uriParam.ID, "status", status, "error", err)
		return
	}

	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	c.JSON(statusCode, gin.H{"status": "updated"})
	slog.InfoContext(c.Request.Context(), "update wallet status completed", "wallet_id", uriParam.ID, "status", statusCode)
}

// GetStatement handles GET /api/v1/wallets/:id/statement
func (h *WalletHandler) GetStatement(c *gin.Context) {
	var uriParam model.WalletURIParam
	if err := c.ShouldBindUri(&uriParam); err != nil {
		slog.WarnContext(c.Request.Context(), "get statement path validation failed", "error", err)
		HandleValidationError(c, err)
		return
	}

	statement, statusCode, err := h.walletService.GetStatement(c.Request.Context(), uriParam.ID)
	if err != nil {
		status := statusCode
		if status == 0 {
			status = http.StatusInternalServerError
		}

		if errors.Is(err, apperror.ErrWalletNotFound) {
			c.JSON(status, gin.H{"error": "wallet not found"})
			slog.WarnContext(c.Request.Context(), "get statement failed: wallet not found", "wallet_id", uriParam.ID, "status", status)
			return
		}
		c.JSON(status, gin.H{"error": "failed to fetch statement"})
		slog.ErrorContext(c.Request.Context(), "get statement failed", "wallet_id", uriParam.ID, "status", status, "error", err)
		return
	}

	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	c.JSON(statusCode, statement)
	slog.InfoContext(c.Request.Context(), "get statement completed", "wallet_id", uriParam.ID, "transaction_count", len(statement.Transactions), "status", statusCode)
}
