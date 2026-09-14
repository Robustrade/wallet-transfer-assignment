package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

// ClaimTransfer attempts to take ownership of an idempotency key by inserting
// the transfer as PENDING, and reports whether it succeeded.
//
// ON CONFLICT DO NOTHING makes the unique index the arbiter: exactly one
// concurrent request can insert a given key, with no check-then-insert window
// for a second to slip through. A false return means the key is already owned
// and the caller should read the existing transfer instead.
//
// It also blocks rather than racing: while another transaction holds an
// uncommitted row with this key, the statement waits for that transaction to
// finish. On commit this returns false; on rollback it claims the key. The
// caller can therefore never observe a PENDING transfer belonging to someone
// else.
func (t *Tx) ClaimTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, bool, error) {
	const query = `
		INSERT INTO transfers (id, idempotency_key, request_hash,
		                       from_wallet_id, to_wallet_id, amount, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING created_at, updated_at`

	err := t.tx.QueryRow(ctx, query,
		transfer.ID, transfer.IdempotencyKey, transfer.RequestHash,
		transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.State,
	).Scan(&transfer.CreatedAt, &transfer.UpdatedAt)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The key is held by an already-committed transfer.
		return domain.Transfer{}, false, nil
	case err != nil:
		return domain.Transfer{}, false, claimError(err, transfer)
	}

	return transfer, true, nil
}

// claimError translates a failed claim into the domain vocabulary. A foreign
// key violation means one of the wallets does not exist; the constraint name
// says which, so the caller can name it rather than making the client guess.
func claimError(err error, transfer domain.Transfer) error {
	code, constraint := pgErrorCode(err)

	if code == sqlstateForeignKeyViolation {
		switch constraint {
		case constraintFromWalletFK:
			return &domain.WalletNotFoundError{WalletID: transfer.FromWalletID}
		case constraintToWalletFK:
			return &domain.WalletNotFoundError{WalletID: transfer.ToWalletID}
		}
	}

	return fmt.Errorf("claim idempotency key: %w", asWalletBusy(err))
}

// TransferByIdempotencyKey reads the transfer that owns a key.
func (t *Tx) TransferByIdempotencyKey(ctx context.Context, key string) (domain.Transfer, error) {
	const query = `
		SELECT id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
		       amount, state, failure_reason, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1`

	var (
		transfer domain.Transfer
		reason   *string
	)

	err := t.tx.QueryRow(ctx, query, key).Scan(
		&transfer.ID, &transfer.IdempotencyKey, &transfer.RequestHash,
		&transfer.FromWalletID, &transfer.ToWalletID, &transfer.Amount,
		&transfer.State, &reason, &transfer.CreatedAt, &transfer.UpdatedAt,
	)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("read transfer for idempotency key: %w", err)
	}

	if reason != nil {
		transfer.FailureReason = *reason
	}

	return transfer, nil
}

// MarkTransferProcessed records a transfer as settled.
//
// The WHERE clause guards the transition in SQL as well as in the domain, so a
// transfer cannot be settled twice even if two code paths tried. The guard does
// not fire in the current flow: the transition commits in the transaction that
// created the row, and no other transaction can see that row beforehand. It is
// defence in depth for the day a PENDING row is committed and left for
// something else to finish — an asynchronous flow, or a reconciler sweeping
// WHERE state = 'PENDING' — when two actors could otherwise settle the same
// transfer. Retrying a whole request is not that case: the claim conflicts on
// the idempotency key and never reaches this statement.
func (t *Tx) MarkTransferProcessed(ctx context.Context, transferID string) error {
	const query = `
		UPDATE transfers
		SET state = 'PROCESSED', updated_at = now()
		WHERE id = $1 AND state = 'PENDING'`

	return t.transition(ctx, query, transferID)
}

// MarkTransferFailed records a transfer as permanently failed, with the reason.
//
// The row is committed rather than rolled back: releasing the idempotency key
// would let a retry re-attempt the debit and possibly succeed, so the same key
// would produce two different answers.
func (t *Tx) MarkTransferFailed(ctx context.Context, transferID, reason string) error {
	const query = `
		UPDATE transfers
		SET state = 'FAILED', failure_reason = $2, updated_at = now()
		WHERE id = $1 AND state = 'PENDING'`

	return t.transition(ctx, query, transferID, reason)
}

// transition runs a guarded state change and reports a row count other than one
// as an invalid transition.
//
// That conflates two causes: the transfer was not PENDING, and no transfer with
// this id exists. Both are programming errors rather than runtime conditions, so
// they are not told apart; separating them would cost a query for a case that
// cannot occur.
func (t *Tx) transition(ctx context.Context, query, transferID string, args ...any) error {
	tag, err := t.tx.Exec(ctx, query, append([]any{transferID}, args...)...)
	if err != nil {
		return fmt.Errorf("transition transfer %s: %w", transferID, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidStateTransition
	}
	return nil
}
