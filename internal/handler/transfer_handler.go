package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

func (h *TransferHandler) Create(c *gin.Context) {
	var req CreateTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	result, err := h.service.CreateTransfer(c.Request.Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})

	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(transferStatusCode(result), toTransferResponse(result))
}

// transferStatusCode picks the HTTP status for a CreateTransfer result.
// FAILED must win regardless of Replayed: an idempotent replay has to return
// the same status the original attempt did, so a replayed FAILED transfer is
// still 422, not 200 -- otherwise retrying the exact same request changes
// its apparent outcome, which breaks idempotent HTTP semantics. A successful
// replay (nothing new created) is 200; a freshly processed transfer is 201.
func transferStatusCode(result *service.TransferResult) int {
	switch {
	case result.Status == domain.TransferFailed:
		return http.StatusUnprocessableEntity
	case result.Replayed:
		return http.StatusOK
	default:
		return http.StatusCreated
	}
}

func (h *TransferHandler) Get(c *gin.Context) {
	result, err := h.service.GetTransfer(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toTransferResponse(result))
}
