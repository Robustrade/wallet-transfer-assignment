package service

import (
	"context"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
)

// TransferInput is the validated, transport-agnostic command to execute a
// transfer. The handler layer maps an HTTP request onto this type.
type TransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         domain.Money
}

// TransferResult is the outcome of a transfer command. Replayed is true when
// the result was served from the idempotency store rather than freshly
// executed, which lets the transport layer surface exactly-once behavior.
type TransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

// WalletBalance is the read model returned by balance queries.
type WalletBalance struct {
	WalletID string
	Balance  domain.Money
}

// TransferService is the application contract consumed by the transport layer.
// Keeping it as an interface keeps handlers thin and testable with fakes, and
// decouples transport from the concrete orchestration in this package.
type TransferService interface {
	// Transfer executes (or replays) a wallet-to-wallet transfer with
	// exactly-once semantics keyed on TransferInput.IdempotencyKey.
	//
	// A recorded business failure (for example insufficient funds) is durable
	// and idempotent: the returned TransferResult carries the FAILED transfer
	// and the error is domain.ErrInsufficientFunds. Input validation failures
	// return the corresponding domain error with a nil result.
	Transfer(ctx context.Context, in TransferInput) (*TransferResult, error)

	// GetTransfer returns a transfer by id, or domain.ErrTransferNotFound.
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)

	// GetWalletBalance returns the current balance of a wallet, or
	// domain.ErrWalletNotFound.
	GetWalletBalance(ctx context.Context, walletID string) (*WalletBalance, error)
}
