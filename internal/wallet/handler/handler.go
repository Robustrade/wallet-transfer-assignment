package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"wallet-transfer/internal/wallet"
	"wallet-transfer/internal/wallet/service"
	"wallet-transfer/internal/wallet/static"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/logger"
	"wallet-transfer/pkg/response"
)

// WalletHandler handles HTTP requests for wallet operations.
type WalletHandler struct {
	svc service.WalletService
}

func NewWalletHandler(svc service.WalletService) *WalletHandler {
	return &WalletHandler{svc: svc}
}

// CreateWallet godoc
// @Description API for creating a new wallet with an optional initial balance.
// @Tags Wallet
// @Accept json
// @Produce json
// @Param request body wallet.CreateWalletRequest true "Request Data"
// @Success 201 {object} response.Success "Wallet created successfully"
// @Failure 400 {object} apperror.AppError "Bad Request"
// @Failure 409 {object} apperror.AppError "Wallet already exists"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /wallets [post]
func (h *WalletHandler) CreateWallet() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "WalletHandler.CreateWallet"
		logger.Info(fn, "create wallet initiated", map[string]interface{}{"ip": c.IP()})

		var req wallet.CreateWalletRequest
		if err := c.BodyParser(&req); err != nil {
			logger.Error(fn, "body parse failed", err, nil)
			return c.Status(fiber.StatusBadRequest).JSON(apperror.BadRequest("invalid request body"))
		}

		w, err := h.svc.CreateWallet(c.Context(), &req)
		if err != nil {
			return handleError(c, fn, err)
		}

		logger.Info(fn, static.WalletCreatedOK, map[string]interface{}{"wallet_id": w.ID})
		return c.Status(fiber.StatusCreated).JSON(response.Created(static.WalletCreatedOK, w))
	}
}

// GetWallet godoc
// @Description API to get wallet details by wallet ID.
// @Tags Wallet
// @Produce json
// @Param id path string true "Wallet ID"
// @Success 200 {object} response.Success "Wallet fetched successfully"
// @Failure 404 {object} apperror.AppError "Wallet not found"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /wallets/{id} [get]
func (h *WalletHandler) GetWallet() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "WalletHandler.GetWallet"
		id := c.Params("id")
		logger.Info(fn, "get wallet initiated", map[string]interface{}{"wallet_id": id})

		w, err := h.svc.GetWallet(c.Context(), id)
		if err != nil {
			return handleError(c, fn, err)
		}

		return c.Status(fiber.StatusOK).JSON(response.OK(static.WalletFetchedOK, w))
	}
}

// GetBalance godoc
// @Description API to get the current balance of a wallet by wallet ID.
// @Tags Wallet
// @Produce json
// @Param id path string true "Wallet ID"
// @Success 200 {object} response.Success "Balance fetched successfully"
// @Failure 404 {object} apperror.AppError "Wallet not found"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /wallets/{id}/balance [get]
func (h *WalletHandler) GetBalance() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "WalletHandler.GetBalance"
		id := c.Params("id")
		logger.Info(fn, "get balance initiated", map[string]interface{}{"wallet_id": id})

		bal, err := h.svc.GetBalance(c.Context(), id)
		if err != nil {
			return handleError(c, fn, err)
		}

		return c.Status(fiber.StatusOK).JSON(response.OK(static.BalanceFetchedOK, bal))
	}
}

// GetAllWallets godoc
// @Description API to get all wallets ordered by creation date.
// @Tags Wallet
// @Produce json
// @Success 200 {object} response.Success "Wallets fetched successfully"
// @Failure 500 {object} apperror.AppError "Internal Server Error"
// @Router /wallets [get]
func (h *WalletHandler) GetAllWallets() fiber.Handler {
	return func(c *fiber.Ctx) error {
		const fn = "WalletHandler.GetAllWallets"
		logger.Info(fn, "get all wallets initiated", nil)

		wallets, err := h.svc.GetAllWallets(c.Context())
		if err != nil {
			return handleError(c, fn, err)
		}

		return c.Status(fiber.StatusOK).JSON(response.OK(static.AllWalletsFetchedOK, wallets))
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
