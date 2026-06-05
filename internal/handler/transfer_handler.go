package handler

import (
	"errors"
	"net/http"
	"wallet-transfer-service/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(
	service *service.TransferService,
) *TransferHandler {

	return &TransferHandler{
		service: service,
	}
}

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`

	FromWalletID string `json:"fromWalletId"`

	ToWalletID string `json:"toWalletId"`

	Amount int64 `json:"amount"`
}

func (h *TransferHandler) CreateTransfer(
	c *gin.Context,
) {

	var req CreateTransferRequest

	if err :=
		c.ShouldBindJSON(&req); err != nil {

		c.JSON(
			http.StatusBadRequest,
			gin.H{
				"error": "Missing or invalid fields in request body",
			},
		)

		return
	}

	fromID, err :=
		uuid.Parse(req.FromWalletID)

	if err != nil {

		c.JSON(
			http.StatusBadRequest,
			gin.H{
				"error": "invalid from wallet id",
			},
		)

		return
	}

	toID, err :=
		uuid.Parse(req.ToWalletID)

	if err != nil {

		c.JSON(
			http.StatusBadRequest,
			gin.H{
				"error": "invalid to wallet id",
			},
		)

		return
	}

	transfer, err :=
		h.service.CreateTransfer(
			c.Request.Context(),
			service.CreateTransferRequest{
				IdempotencyKey: req.IdempotencyKey,

				FromWalletID: fromID,

				ToWalletID: toID,

				Amount: req.Amount,
			},
		)

	if err != nil {

		switch {

		case errors.Is(err, service.ErrInsufficientFunds):

			c.JSON(
				http.StatusBadRequest,
				gin.H{
					"error": "insufficient funds",
				},
			)

		case errors.Is(err, service.ErrWalletNotFound):
			c.JSON(
				http.StatusNotFound,
				gin.H{
					"error": "wallet not found",
				},
			)

		default:
			c.JSON(
				http.StatusInternalServerError,
				gin.H{
					"error": "internal server error",
				},
			)
		}

		return
	}

	c.JSON(
		http.StatusCreated,
		transfer,
	)
}
