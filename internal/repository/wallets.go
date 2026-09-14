package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

// LockWallet reads a wallet and holds its row until the transaction ends.
//
// The balance it returns is only meaningful because the lock is held: reading
// and writing under the same lock is what closes the read-then-write window
// that would otherwise let two concurrent debits both see enough money.
//
// FOR NO KEY UPDATE rather than FOR UPDATE, and the difference is not cosmetic.
// Inserting a transfer takes a FOR KEY SHARE lock on both wallet rows, because
// that is how PostgreSQL stops a referenced row being deleted underneath a
// foreign key. FOR UPDATE conflicts with FOR KEY SHARE, so a transaction that
// claims the idempotency key and then reaches for FOR UPDATE is asking to
// upgrade a lock it already shares with every other in-flight transfer touching
// that wallet — and two transactions each waiting for the other to release a
// shared lock is a deadlock that no amount of lock ordering can prevent.
//
// FOR NO KEY UPDATE does not conflict with FOR KEY SHARE, but does conflict
// with itself, which is exactly the mutual exclusion a debit needs. It is also
// the honest lock strength: a transfer changes balance and updated_at, never
// the wallet's key.
//
// Callers must lock wallets in a consistent order — see LockWalletsInOrder.
func (t *Tx) LockWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	const query = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR NO KEY UPDATE`

	var wallet domain.Wallet
	err := t.tx.QueryRow(ctx, query, walletID).Scan(
		&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Wallet{}, &domain.WalletNotFoundError{WalletID: walletID}
	case err != nil:
		return domain.Wallet{}, fmt.Errorf("lock wallet %s: %w", walletID, asWalletBusy(err))
	}

	return wallet, nil
}

// LockWalletsInOrder locks both wallets in ascending id order and returns them
// keyed by id.
//
// The ordering is the point. Two transfers moving money in opposite directions
// between the same pair would otherwise each hold the row the other needs, and
// deadlock; locking in a consistent order means one simply waits. The order is
// imposed here in Go rather than with ORDER BY so it is a property of the code
// rather than of whatever plan the query planner happens to choose.
func (t *Tx) LockWalletsInOrder(ctx context.Context, firstID, secondID string) (map[string]domain.Wallet, error) {
	lockOrder := []string{firstID, secondID}
	if secondID < firstID {
		lockOrder = []string{secondID, firstID}
	}

	wallets := make(map[string]domain.Wallet, len(lockOrder))
	for _, walletID := range lockOrder {
		wallet, err := t.LockWallet(ctx, walletID)
		if err != nil {
			return nil, err
		}
		wallets[walletID] = wallet
	}

	return wallets, nil
}

// DebitWallet removes amount from a wallet's stored balance.
//
// The arithmetic is relative rather than absolute. Under the row lock an
// absolute write would be equally correct, so this is defensive style: it stays
// correct even if the lock were ever dropped, and it cannot overwrite a change
// this transaction did not see.
//
// A debit that would take the balance below zero violates the CHECK constraint
// and aborts the transaction. Callers check sufficiency under the lock first;
// reaching that constraint means a bug, not insufficient funds.
func (t *Tx) DebitWallet(ctx context.Context, walletID string, amount int64) error {
	const query = `
		UPDATE wallets
		SET balance = balance - $2, updated_at = now()
		WHERE id = $1`

	return t.adjustBalance(ctx, query, walletID, amount)
}

// CreditWallet adds amount to a wallet's stored balance.
func (t *Tx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	const query = `
		UPDATE wallets
		SET balance = balance + $2, updated_at = now()
		WHERE id = $1`

	return t.adjustBalance(ctx, query, walletID, amount)
}

func (t *Tx) adjustBalance(ctx context.Context, query, walletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, query, walletID, amount)
	if err != nil {
		return fmt.Errorf("adjust balance of wallet %s: %w", walletID, asWalletBusy(err))
	}
	if tag.RowsAffected() != 1 {
		return &domain.WalletNotFoundError{WalletID: walletID}
	}
	return nil
}
