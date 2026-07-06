package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/gin-gonic/gin"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req domain.TransferRequest

	// 1. Bind JSON payload
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[Handler] CreateTransfer - Invalid payload: %v", err)
		// Use fmt.Sprintf to combine your custom message with the internal error details
		RequestError(c, http.StatusBadRequest, fmt.Sprintf("Invalid request payload: %v", err))
		return
	}

	log.Printf("[Handler] CreateTransfer - Received request: %+v", req)

	// 2. Additional Business Validation
	if req.FromWalletId == req.ToWalletId {
		RequestError(c, http.StatusBadRequest, "fromWalletId and toWalletId cannot be identical")
		return
	}

	// 3. Call Service layer
	resp, err := h.service.ExecuteTransfer(c.Request.Context(), req)
	if err != nil {
		log.Printf("[Handler] CreateTransfer - Error executing transfer: %v", err)

		// Special Case: We hit an idempotency block on a failed transaction.
		// Return the 422 status, but include the cached data.
		if errors.Is(err, domain.ErrInsufficientFunds) && resp != nil {
			RequestErrorWithData(c, http.StatusUnprocessableEntity, err.Error(), resp)
			return
		}

		// Standard error mapping
		switch {
		case errors.Is(err, domain.ErrInvalidIdempotency):
			RequestError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, domain.ErrInsufficientFunds):
			RequestError(c, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, domain.ErrWalletNotFound):
			RequestError(c, http.StatusNotFound, err.Error())
		default:
			// Mask internal database errors from the client
			RequestError(c, http.StatusInternalServerError, "Internal server error")
		}
		return
	}

	// 4. Return formatted JSON response
	log.Printf("[Handler] CreateTransfer - Success, returning resp: %+v", resp)
	RequestSuccess(c, http.StatusOK, resp)
}
