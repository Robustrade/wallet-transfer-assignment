package construct

import (
	"context"
	"errors"
	"net/http"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
)

var (
	// router errors
	ErrRouteNotFound = utils.NewCustomError(http.StatusNotFound, "route not found")

	// middleware errors
	ErrServerBusy = utils.NewCustomError(http.StatusServiceUnavailable, "server is busy, please retry")

	// handler errors
	ErrInternal            = utils.NewCustomError(http.StatusInternalServerError, "something went wrong")
	ErrInvalidRequestBody  = utils.NewCustomError(http.StatusBadRequest, "invalid request body")
	ErrMissingTransferData = utils.NewCustomError(http.StatusBadRequest, "from_wallet_id, to_wallet_id and a positive amount are required")
	ErrMissingWalletID     = utils.NewCustomError(http.StatusBadRequest, "wallet id is required")
	ErrInvalidPagination   = utils.NewCustomError(http.StatusBadRequest, "limit and offset must be valid integers")

	// service errors
	ErrSameWallet             = utils.NewCustomError(http.StatusBadRequest, domain.ErrSameWallet.Error())
	ErrTransferFailed         = utils.NewCustomError(http.StatusInternalServerError, domain.ErrTransferFailed.Error())
	ErrInsufficientBalance    = utils.NewCustomError(http.StatusUnprocessableEntity, domain.ErrInsufficientBalance.Error())
	ErrWalletNotFound         = utils.NewCustomError(http.StatusNotFound, domain.ErrWalletNotFound.Error())
	ErrIdempotencyKeyConflict = utils.NewCustomError(http.StatusConflict, domain.ErrIdempotencyKeyConflict.Error())
	ErrTransferTimeout        = utils.NewCustomError(http.StatusGatewayTimeout, "transfer timed out, retry with the same idempotency key to check status")
)

func MapTransferError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInsufficientBalance):
		return ErrInsufficientBalance
	case errors.Is(err, domain.ErrWalletNotFound):
		return ErrWalletNotFound
	case errors.Is(err, domain.ErrSameWallet):
		return ErrSameWallet
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		return ErrIdempotencyKeyConflict
	case errors.Is(err, context.DeadlineExceeded):
		return ErrTransferTimeout
	default:
		return ErrTransferFailed
	}
}
