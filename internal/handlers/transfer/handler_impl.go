package handlers

import (
	"net/http"

	"wallet-transfer-assignment/internal/api/requests"
	"wallet-transfer-assignment/internal/api/responses"
	"wallet-transfer-assignment/internal/controller/transfer"
	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/utils"

	"github.com/gin-gonic/gin"
)

// TransferHandlerImpl coordinates the translation between HTTP and Domain layers.
type TransferHandlerImpl struct {
	transferController transfer.TransferController
}

// NewTransferHandler returns a new instance of the TransferHandler.
func NewTransferHandler(transferController transfer.TransferController) TransferHandler {
	return &TransferHandlerImpl{
		transferController: transferController,
	}
}

// CreateTransfer handles the POST /transfers request.
// It maps application-layer results to appropriate HTTP status codes.
// Note: This handler is wrapped by IdempotencyMiddleware, which handles request
// deduplication and response caching based on the results returned here.
func (h *TransferHandlerImpl) CreateTransfer(c *gin.Context) {
	// 1. Schema Validation: Checks for malformed JSON or missing required fields via GIN tags.
	var req requests.TransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.GeneralAPIValidationError(c, err.Error())
		return
	}

	// 2. Extract Idempotency Key (verified as non-empty by middleware).
	idempotencyKey := c.GetHeader("Idempotency-Key")

	// 3. Domain Model Construction.
	transferObj := &models.Transfer{
		ID:             idempotencyKey,
		IdempotencyKey: idempotencyKey,
		SourceWalletID: req.SourceWalletID,
		DestWalletID:   req.DestWalletID,
		Amount:         req.Amount,
	}

	// 4. Controller Execution.
	result, err := h.transferController.DoWalletTransfer(transferObj)
	if err != nil {
		// Business Logic failures (e.g., Insufficient Funds) -> 422.
		// These failures are permanent and CACHED by the middleware.
		if transfer.IsBusinessError(err) {
			meta := gin.H{
				"transfer_id": idempotencyKey,
			}

			if result != nil {
				meta["transfer_id"] = result.ID
				meta["status"] = string(result.Status)
			}

			utils.CustomAPIErrorWithMeta(c, http.StatusUnprocessableEntity, err.Error(), meta)
			return
		}

		// Client Validation failures (e.g., Self-Transfer, Missing Wallet) -> 400.
		// These are also permanent and CACHED by the middleware.
		if transfer.IsValidationError(err) {
			utils.GeneralAPIValidationError(c, err.Error())
			return
		}

		// System Failures (e.g., Database Connection Loss) -> 500.
		// These trigger a RESET in the middleware, allowing the client to retry the SAME key later.
		utils.GeneralAPIInternalError(c, err.Error())
		return
	}

	// 5. Success Path -> 200 OK.
	// The full JSON response is intercepted and CACHED by the middleware.
	resp := responses.TransferResponse{
		TransferID: result.ID,
		Status:     string(result.Status),
	}
	utils.GenerateSuccessResponse(c, resp)
}
