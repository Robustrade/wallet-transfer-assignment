package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
)

const rollbackTimeout = 5 * time.Second

// TransferExecutor is the pgx-backed implementation of repository.TransferExecutor.
type TransferExecutor struct {
	pool *pgxpool.Pool
}

func NewTransferExecutor(pool *pgxpool.Pool) *TransferExecutor {
	return &TransferExecutor{pool: pool}
}

// Execute performs the debit, credit, ledger writes and state transition in a
// single transaction. Three deliberate choices:
//
//   - Wallet UPDATEs go in ascending wallet-id order, not source-first: an
//     UPDATE holds its row lock until commit, so A->B racing B->A would
//     otherwise deadlock. When the credit sorts first it lands before the
//     debit is known to succeed — a failed debit rolls the whole transaction
//     back, so nothing leaks.
//
//   - The debit is a conditional UPDATE (WHERE balance >= $amount). Under
//     READ COMMITTED, Postgres re-evaluates that condition against the current
//     row version once the lock is granted, so the check and the decrement are
//     atomic with no separate read. Zero rows means insufficient funds (also a
//     missing wallet, which cannot happen today).
//
//   - The state change is a compare-and-swap (WHERE state = PENDING), so a
//     second Execute against the same transfer fails instead of re-applying it.
func (e *TransferExecutor) Execute(ctx context.Context, transfer *domain.Transfer, debit, credit domain.LedgerEntry) error {
	if err := checkEntries(transfer, debit, credit); err != nil {
		return err
	}

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}

	// WithoutCancel so the rollback still runs on a dead context, but bounded:
	// on its own it also strips the deadline, and a stalled connection would
	// then hold this rollback — and its pool slot — open indefinitely.
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	// The wallet updates go inside a savepoint so that insufficient funds can
	// be undone without losing the outer transaction. That lets the FAILED
	// state be recorded in the *same* transaction that declined to move the
	// money, instead of a second write afterwards that a crash could lose.
	moved, err := tx.Begin(ctx)
	if err != nil {
		return err
	}

	first, second := debitWallet, creditWallet
	if transfer.FromWalletID > transfer.ToWalletID {
		first, second = creditWallet, debitWallet
	}

	moveErr := first(ctx, moved, transfer)
	if moveErr == nil {
		moveErr = second(ctx, moved, transfer)
	}

	if errors.Is(moveErr, domain.ErrInsufficientFunds) {
		// Undo the leg that may already have applied (when the credit sorts
		// first), then mark the transfer FAILED and commit that decision.
		if err := moved.Rollback(ctx); err != nil {
			return err
		}
		if err := setState(ctx, tx, transfer, domain.TransferFailed); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return domain.ErrInsufficientFunds
	}
	if moveErr != nil {
		return moveErr
	}
	if err := moved.Commit(ctx); err != nil {
		return err
	}

	const insertLedgerEntry = `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`
	for _, entry := range []domain.LedgerEntry{debit, credit} {
		if _, err := tx.Exec(ctx, insertLedgerEntry,
			entry.ID, entry.WalletID, entry.TransferID, entry.Type, entry.Amount, entry.CreatedAt); err != nil {
			return err
		}
	}

	if err := setState(ctx, tx, transfer, domain.TransferProcessed); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// setState is the compare-and-swap out of PENDING. Zero rows means someone
// else already moved this transfer, so this attempt must not apply.
func setState(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer, to domain.TransferState) error {
	tag, err := tx.Exec(ctx,
		`UPDATE transfers SET state = $3, updated_at = now() WHERE id = $1 AND state = $2`,
		transfer.ID, domain.TransferPending, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvalidStateTransition
	}
	return nil
}

// checkEntries rejects a debit/credit pair that disagrees with the transfer.
// The balance updates are driven by transfer, but the ledger rows are written
// from these entries, so without this a caller bug could move the right money
// and record a reversed, cross-wallet, or cross-transfer pair against it.
func checkEntries(transfer *domain.Transfer, debit, credit domain.LedgerEntry) error {
	switch {
	case debit.Type != domain.LedgerDebit, credit.Type != domain.LedgerCredit:
		return fmt.Errorf("%w: entry types are not one debit and one credit", domain.ErrInconsistentLedger)
	case debit.TransferID != transfer.ID, credit.TransferID != transfer.ID:
		return fmt.Errorf("%w: entries reference a different transfer than %s", domain.ErrInconsistentLedger, transfer.ID)
	case debit.WalletID != transfer.FromWalletID:
		return fmt.Errorf("%w: debit wallet %s is not the source wallet", domain.ErrInconsistentLedger, debit.WalletID)
	case credit.WalletID != transfer.ToWalletID:
		return fmt.Errorf("%w: credit wallet %s is not the destination wallet", domain.ErrInconsistentLedger, credit.WalletID)
	case debit.Amount != transfer.Amount, credit.Amount != transfer.Amount:
		return fmt.Errorf("%w: entry amounts do not equal the transfer amount", domain.ErrInconsistentLedger)
	}
	return nil
}

func debitWallet(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) error {
	tag, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance - $1, updated_at = now() WHERE id = $2 AND balance >= $1`,
		transfer.Amount, transfer.FromWalletID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInsufficientFunds
	}
	return nil
}

func creditWallet(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) error {
	_, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = balance + $1, updated_at = now() WHERE id = $2`,
		transfer.Amount, transfer.ToWalletID)
	return err
}
