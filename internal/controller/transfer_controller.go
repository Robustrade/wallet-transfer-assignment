package controller

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/middleware"
	"github.com/Robustrade/wallet-transfer-assignment/internal/response"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         *int64 `json:"amount"`
}

type transferResponse struct {
	TransferID     string    `json:"transferId"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason"`
	CreatedAt      time.Time `json:"createdAt"`
}

func toTransferResponse(t *domain.Transfer) transferResponse {
	resp := transferResponse{
		TransferID:     t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt,
	}
	if t.FailureReason != "" {
		reason := string(t.FailureReason)
		resp.FailureReason = &reason
	}
	return resp
}

func CreateTransfer(c *gin.Context) {
	var req createTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "", "request body must be valid JSON with the expected field types")
		return
	}
	if req.Amount == nil {
		response.BadRequest(c, "amount", "is required")
		return
	}

	svc := service.TransferService{}
	transfer, replayed, err := svc.CreateTransfer(c.Request.Context(), domain.TransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         *req.Amount,
	})
	if err != nil {
		response.Error(c, err)
		return
	}

	slog.Info("transfer",
		middleware.RequestIDKey, c.GetString(middleware.RequestIDKey),
		"transfer_id", transfer.ID,
		"idempotency_key", transfer.IdempotencyKey,
		"status", transfer.Status,
		"replayed", replayed,
	)

	if replayed {
		c.Header("Idempotent-Replayed", "true")
	}

	body := toTransferResponse(transfer)
	if transfer.Status == domain.StatusFailed {
		response.ErrorWithData(c, http.StatusUnprocessableEntity, string(transfer.FailureReason), "source wallet has insufficient funds", "transfer", body)
		return
	}
	response.Created(c, body)
}
