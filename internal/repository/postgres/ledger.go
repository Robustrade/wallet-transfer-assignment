package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// LedgerRepo is the PostgreSQL implementation of
// repository.LedgerRepository. Ledger rows are append-only at this
// layer — there is no Update method by design.
type LedgerRepo struct{}

func NewLedgerRepo() *LedgerRepo { return &LedgerRepo{} }

// Insert appends one ledger entry. The schema's UNIQUE (transfer_id,
// type) constraint prevents two DEBITs or two CREDITs against the
// same transfer; a buggy caller attempting that would get a unique
// violation here.
//
// RETURNING gives back the auto-assigned id and timestamp so the
// caller has a fully populated entity without a second round trip.
func (r *LedgerRepo) Insert(ctx context.Context, exec repository.Executor, e *domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`
	row := exec.QueryRowContext(ctx, q, e.TransferID, e.WalletID, e.Type, e.Amount)
	if err := row.Scan(&e.ID, &e.CreatedAt); err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

// ListByTransfer returns the entries for transferID ordered by id
// (i.e. insertion order). The result is always either empty (transfer
// never reached PROCESSED, e.g. FAILED on insufficient funds) or has
// exactly two rows (DEBIT + CREDIT for a successful transfer).
//
// Tests assert that the two rows sum to zero; the schema does not
// enforce that mathematically, but it does enforce the cardinality
// via the UNIQUE constraint.
func (r *LedgerRepo) ListByTransfer(ctx context.Context, exec repository.Executor, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	const q = `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY id`
	rows, err := exec.QueryContext(ctx, q, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
