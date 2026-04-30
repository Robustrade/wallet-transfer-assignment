// Package request holds inbound DTOs for the transfer service.
package request

import (
	"strings"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
)

// CreateTransferReq is the JSON body of POST /transfers.
//
// `Amount` is decoded as a JSON number per the assignment spec.
// Production-grade APIs would use a string to avoid client-side float
// precision loss; that is documented as a known tradeoff.
type CreateTransferReq struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// MaxIdempotencyKeyLen and MaxWalletIDLen mirror the column widths in the DB
// schema so length violations are caught at the API as 400 rather than as a
// generic 500 from a Postgres VARCHAR overflow.
const (
	MaxIdempotencyKeyLen = 255
	MaxWalletIDLen       = 64
)

// Validate performs syntactic validation of the request body. It returns
// an *apperr.AppError so the handler can map to the right HTTP status.
//
// Wallet IDs are matched against the database verbatim, so leading/trailing
// whitespace would produce a confusing WALLET_NOT_FOUND. Reject those at the
// boundary instead of silently trimming — the client should send canonical
// IDs, and a quiet trim could mask a marshalling bug on their end.
func (r *CreateTransferReq) Validate() error {
	if r.IdempotencyKey == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return apperr.ErrInvalidIdempotencyKey.WithMessage("idempotencyKey is required")
	}
	if len(r.IdempotencyKey) > MaxIdempotencyKeyLen {
		return apperr.ErrInvalidIdempotencyKey.WithMessage("idempotencyKey is too long")
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return apperr.ErrInvalidWalletID.WithMessage("fromWalletId and toWalletId are required")
	}
	if r.FromWalletID != strings.TrimSpace(r.FromWalletID) ||
		r.ToWalletID != strings.TrimSpace(r.ToWalletID) {
		return apperr.ErrInvalidWalletID.WithMessage("wallet id must not contain leading or trailing whitespace")
	}
	if len(r.FromWalletID) > MaxWalletIDLen || len(r.ToWalletID) > MaxWalletIDLen {
		return apperr.ErrInvalidWalletID.WithMessage("wallet id is too long")
	}
	if r.FromWalletID == r.ToWalletID {
		return apperr.ErrSameWallet
	}
	if r.Amount <= 0 {
		return apperr.ErrInvalidAmount.WithMessage("amount must be a positive integer")
	}
	return nil
}

// AmountMoney returns Amount as a money.Money value.
func (r *CreateTransferReq) AmountMoney() money.Money {
	return money.FromMinor(r.Amount)
}
