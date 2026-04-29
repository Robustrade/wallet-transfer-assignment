package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"wallet-transfer/internal/transfer"
	"wallet-transfer/internal/transfer/service"
	"wallet-transfer/internal/transfer/static"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/logger"
	"wallet-transfer/pkg/response"
)

// TransferHandler handles HTTP requests for transfer operations.
type TransferHandler struct {
	svc service.TransferService
}

func NewTransferHandler(svc service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// CreateTransfer godoc
// @Description API for creating a wallet-to-wallet transfer. Supports idempotent requests via idempotencyKey.
// @Tags Transfer
// @Accept json
// @Produce json
// @Param request body transfer.CreateTransferRequest true "Request Data"
// @Success 201 {object} response.Success "Transfer created successfully"
// @Failure 400 {object} apperror.AppError "Bad Request"
// @Failure 404 {object} apperror.AppError "Wallet not found"
// @Failure 409 {object} apperror.AppError "Idempotency key reused with different parameters"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /transfers [post]
func (h *TransferHandler) CreateTransfer() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "TransferHandler.CreateTransfer"
		logger.Info(fn, "create transfer initiated", map[string]interface{}{"ip": c.IP()})

		var req transfer.CreateTransferRequest
		if err := c.BodyParser(&req); err != nil {
			logger.Error(fn, "body parse failed", err, nil)
			return c.Status(fiber.StatusBadRequest).JSON(apperror.BadRequest("invalid request body"))
		}

		logger.Debug(fn, "transfer request", map[string]interface{}{
			"from":            req.FromWalletID,
			"to":              req.ToWalletID,
			"amount":          req.Amount,
			"idempotency_key": req.IdempotencyKey,
		})

		resp, err := h.svc.CreateTransfer(c.Context(), &req)
		if err != nil {
			return handleError(c, fn, err)
		}

		logger.Info(fn, static.TransferCreated, map[string]interface{}{
			"transfer_id": resp.Transfer.ID,
			"status":      resp.Transfer.Status,
		})
		return c.Status(fiber.StatusCreated).JSON(response.Created(static.TransferCreated, resp))
	}
}

// GetTransfer godoc
// @Description API to get transfer details along with ledger entries by transfer ID.
// @Tags Transfer
// @Produce json
// @Param id path string true "Transfer ID"
// @Success 200 {object} response.Success "Transfer fetched successfully"
// @Failure 404 {object} apperror.AppError "Transfer not found"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /transfers/{id} [get]
func (h *TransferHandler) GetTransfer() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "TransferHandler.GetTransfer"
		id := c.Params("id")
		logger.Info(fn, "get transfer initiated", map[string]interface{}{"transfer_id": id})

		resp, err := h.svc.GetTransfer(c.Context(), id)
		if err != nil {
			return handleError(c, fn, err)
		}

		return c.Status(fiber.StatusOK).JSON(response.OK(static.TransferFetched, resp))
	}
}

// GetAllTransfers godoc
// @Description API to get all transfers ordered by creation date.
// @Tags Transfer
// @Produce json
// @Success 200 {object} response.Success "Transfers fetched successfully"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /transfers [get]
func (h *TransferHandler) GetAllTransfers() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "TransferHandler.GetAllTransfers"
		logger.Info(fn, "get all transfers initiated", nil)

		transfers, err := h.svc.GetAllTransfers(c.Context())
		if err != nil {
			return handleError(c, fn, err)
		}

		return c.Status(fiber.StatusOK).JSON(response.OK(static.AllTransfersFetched, transfers))
	}
}

func handleError(c *fiber.Ctx, fn string, err error) error {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		logger.Warn(fn, appErr.Message, map[string]interface{}{"code": appErr.Code})
		return c.Status(appErr.Code).JSON(appErr)
	}
	logger.Error(fn, "unexpected error", err, nil)
	return c.Status(fiber.StatusInternalServerError).JSON(apperror.InternalServerError("unexpected error"))
}
