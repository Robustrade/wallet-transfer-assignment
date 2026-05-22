package handlers

import (
	"wallet-transfer-assignment/internal/api/responses"
	"wallet-transfer-assignment/internal/controller/idempotency"
	"wallet-transfer-assignment/internal/utils"

	"github.com/gin-gonic/gin"
)

type IdempotencyHandlerImpl struct {
	idempotencyController idempotency.IdempotencyController
}

func NewIdempotencyHandler(idempotencyController idempotency.IdempotencyController) IdempotencyHandler {
	return &IdempotencyHandlerImpl{
		idempotencyController: idempotencyController,
	}
}

func (h *IdempotencyHandlerImpl) GenerateID(c *gin.Context) {
	newID, err := h.idempotencyController.GenerateID()
	if err != nil {
		utils.GeneralAPIInternalError(c, err.Error())
		return
	}

	utils.GenerateSuccessResponse(c, responses.IdResponse{ID: newID})
}
