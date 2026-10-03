package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/domain"
)

// Tx is the transaction-scoped repository the transfer workflow runs on.
type Tx interface {
	InsertTransfer(ctx context.Context, transfer domain.Transfer) (bool, error)
	FindTransferByKey(ctx context.Context, key string) (domain.Transfer, error)
	LockWallet(ctx context.Context, walletID string) (domain.Wallet, error)
	DebitWallet(ctx context.Context, walletID string, amount int64) error
	CreditWallet(ctx context.Context, walletID string, amount int64) error
	InsertLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error
	UpdateTransferState(ctx context.Context, transferID string, state domain.TransferState) error
}

// Repository runs fn in one database transaction, passing it a
// transaction-scoped repository of type T.
type Repository[T Tx] interface {
	InTx(ctx context.Context, fn func(T) error) error
}

// TransferService runs the transfer workflow (design §5).
type TransferService struct {
	inTx func(ctx context.Context, fn func(Tx) error) error
}

// New returns a TransferService that runs on repo. It is generic so the
// repository can pass its own transaction type to fn without importing this
// package (design §3).
func New[T Tx](repo Repository[T]) *TransferService {
	return &TransferService{
		inTx: func(ctx context.Context, fn func(Tx) error) error {
			return repo.InTx(ctx, func(tx T) error { return fn(tx) })
		},
	}
}

// Transfer runs a validated request in one transaction and returns the
// transfer in its final state.
func (s *TransferService) Transfer(ctx context.Context, req domain.TransferRequest) (domain.Transfer, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("generate transfer ID: %w", err)
	}
	transfer := domain.NewTransfer(id.String(), req)

	err = s.inTx(ctx, func(tx Tx) error {
		inserted, err := tx.InsertTransfer(ctx, transfer)
		if err != nil {
			return err
		}
		if !inserted {
			// The key is taken: replay its transfer if the body is the same.
			existing, err := tx.FindTransferByKey(ctx, req.IdempotencyKey)
			if err != nil {
				return err
			}
			if !existing.Matches(req) {
				return domain.ErrIdempotencyKeyReused
			}
			transfer = existing
			return nil
		}

		source, err := lockWallets(ctx, tx, transfer)
		if err != nil {
			return err
		}
		if source.Balance < transfer.Amount {
			return fail(ctx, tx, &transfer)
		}
		return process(ctx, tx, &transfer)
	})
	if err != nil {
		return domain.Transfer{}, err
	}
	return transfer, nil
}

// lockWallets locks both wallets in ascending ID order and returns the source
// wallet. The balance is read under the lock, so no other transfer can spend
// it before this one commits. Locking in one order means transfers in
// opposite directions cannot deadlock (design §7).
func lockWallets(ctx context.Context, tx Tx, transfer domain.Transfer) (domain.Wallet, error) {
	ids := []string{transfer.FromWalletID, transfer.ToWalletID}
	slices.Sort(ids)
	var source domain.Wallet
	for _, id := range ids {
		wallet, err := tx.LockWallet(ctx, id)
		if err != nil {
			return domain.Wallet{}, err
		}
		if id == transfer.FromWalletID {
			source = wallet
		}
	}
	return source, nil
}

// fail marks the transfer FAILED, leaving balances and the ledger unchanged.
func fail(ctx context.Context, tx Tx, transfer *domain.Transfer) error {
	if err := transfer.MarkFailed(); err != nil {
		return err
	}
	return tx.UpdateTransferState(ctx, transfer.ID, transfer.State)
}

// process moves the balances, writes both ledger entries, and marks the
// transfer PROCESSED.
func process(ctx context.Context, tx Tx, transfer *domain.Transfer) error {
	if err := tx.DebitWallet(ctx, transfer.FromWalletID, transfer.Amount); err != nil {
		return err
	}
	if err := tx.CreditWallet(ctx, transfer.ToWalletID, transfer.Amount); err != nil {
		return err
	}
	for _, entry := range transfer.LedgerEntries() {
		if err := tx.InsertLedgerEntry(ctx, entry); err != nil {
			return err
		}
	}
	if err := transfer.MarkProcessed(); err != nil {
		return err
	}
	return tx.UpdateTransferState(ctx, transfer.ID, transfer.State)
}
