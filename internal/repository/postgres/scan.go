package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// Column list shared by all transfer reads/writes with RETURNING.
const transferCols = `id, idempotency_key, from_wallet_id, to_wallet_id, amount,
	state, failure_reason, attempts, claimed_at, worker_id, created_at, updated_at`

func scanTransfer(row pgx.Row) (domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(
		&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&t.State, &t.FailureReason, &t.Attempts, &t.ClaimedAt, &t.WorkerID,
		&t.CreatedAt, &t.UpdatedAt,
	)
	return t, err
}

func scanWallet(row pgx.Row) (domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.IsActive, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// classify maps raw driver/pg errors to the errors the service reasons about.
func classify(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err // callers turn this into a domain not-found where relevant
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23503": // foreign_key_violation: a referenced wallet does not exist
			return domain.ErrWalletNotFound
		case "23514": // check_violation: the balance would go negative
			if pg.ConstraintName == "balance_non_negative" {
				return domain.ErrInsufficientFunds
			}
		case "23505": // unique_violation
			if pg.ConstraintName == "ledger_one_entry_per_type" {
				return repository.ErrLedgerDuplicate
			}
		}
	}
	return err
}
