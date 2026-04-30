package apperr

import "net/http"

// Transfer-related sentinel errors.
var (
	ErrSameWallet = &AppError{
		Code:       "SAME_WALLET",
		HTTPStatus: http.StatusBadRequest,
		Message:    "from and to wallet must differ",
	}

	ErrInvalidAmount = &AppError{
		Code:       "INVALID_AMOUNT",
		HTTPStatus: http.StatusBadRequest,
		Message:    "amount is invalid",
	}

	ErrInvalidIdempotencyKey = &AppError{
		Code:       "INVALID_IDEMPOTENCY_KEY",
		HTTPStatus: http.StatusBadRequest,
		Message:    "idempotencyKey is invalid",
	}

	ErrInvalidWalletID = &AppError{
		Code:       "INVALID_WALLET_ID",
		HTTPStatus: http.StatusBadRequest,
		Message:    "wallet id is invalid",
	}

	ErrIdempotencyConflict = &AppError{
		Code:       "IDEMPOTENCY_CONFLICT",
		HTTPStatus: http.StatusConflict,
		Message:    "idempotency key reused with a different request body",
	}

	ErrBadJSON = &AppError{
		Code:       "BAD_JSON",
		HTTPStatus: http.StatusBadRequest,
		Message:    "request body is not valid JSON",
	}

	ErrPayloadTooLarge = &AppError{
		Code:       "PAYLOAD_TOO_LARGE",
		HTTPStatus: http.StatusRequestEntityTooLarge,
		Message:    "request body exceeds maximum allowed size",
	}

	ErrInternal = &AppError{
		Code:       "INTERNAL_ERROR",
		HTTPStatus: http.StatusInternalServerError,
		Message:    "internal server error",
	}
)
