// Package repoiface defines the repository contracts the service layer depends on.
//
// Interfaces are co-located with their consumer (the service layer); the
// postgres implementation lives in the sibling `postgres/` package and
// satisfies these interfaces.
package repoiface

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
)

// BalanceDelta is a signed change applied to a single wallet's balance.
type BalanceDelta struct {
	WalletID string
	Delta    money.Money
}

// WalletRepoIface persists and reads wallet rows.
type WalletRepoIface interface {
	// LockByIDs acquires a row-level exclusive lock on every requested wallet,
	// in deterministic order (by id), and returns them. The lock is held until
	// the surrounding transaction commits or rolls back.
	//
	// If any of the requested IDs is missing, returns apperr.ErrWalletNotFound.
	LockByIDs(ctx context.Context, ids []string) (model.Wallets, error)

	// ApplyBalanceDeltas applies all supplied deltas to their wallets in a
	// single SQL statement. The DB CHECK (balance_minor >= 0) is the last-line
	// invariant; callers MUST validate balance under the row lock first.
	ApplyBalanceDeltas(ctx context.Context, deltas []BalanceDelta) error
}
