package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/middleware"
)

// headerReplayed marks a response that returns a stored result.
const headerReplayed = "Idempotent-Replayed"

type transferHandler struct {
	svc TransferCreator
}

// create handles POST /transfers: decode and validate, call the service, map
// the result. The HTTP status is derived from the transfer's stored status,
// so a replay naturally returns the same status as the original response.
func (h *transferHandler) create(c *gin.Context) {
	cmd, err := decodeCreateTransfer(c.Request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Set(middleware.KeyIdempotencyKey, cmd.IdempotencyKey)

	res, err := h.svc.CreateTransfer(c.Request.Context(), cmd)
	if err != nil {
		respondError(c, err)
		return
	}

	var status int
	switch res.Transfer.Status {
	case domain.StatusProcessed:
		status = http.StatusCreated
		c.Set(middleware.KeyOutcome, "processed")
	case domain.StatusFailed:
		status = http.StatusUnprocessableEntity
		c.Set(middleware.KeyOutcome, "failed")
	default:
		respondError(c, errUnexpectedStatus(res.Transfer.Status))
		return
	}
	if res.Replayed {
		c.Header(headerReplayed, "true")
		c.Set(middleware.KeyOutcome, "replayed")
	}
	c.Set(middleware.KeyTransferID, res.Transfer.ID.String())
	c.JSON(status, toTransferResponse(res.Transfer))
}
