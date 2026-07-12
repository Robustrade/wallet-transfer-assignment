package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

const transferColumns = `id, request_hash, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at`

// TransferRepo persists transfers and their ledger entries.
type TransferRepo struct {
	pool *pgxpool.Pool
}

// NewTransferRepo constructs a TransferRepo.
func NewTransferRepo(pool *pgxpool.Pool) *TransferRepo {
	return &TransferRepo{pool: pool}
}

// InsertTransfer claims id as a new transfer via INSERT ... ON CONFLICT
// DO NOTHING, writing its final, immutable status in the same statement —
// by the time this is called, the caller already holds the wallet locks
// and has decided the outcome. It returns (nil, nil) if id was already
// claimed by another transfer (the conflict-resolve path owns what
// happens next), and a non-nil error only for genuine failures.
func (r *TransferRepo) InsertTransfer(ctx context.Context, tx repository.Tx, id int64, requestHash, fromWalletID, toWalletID string, amount decimal.Decimal, status domain.Status, failureReason *domain.ErrorCode) (*domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO transfers (id, request_hash, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
		ON CONFLICT (id) DO NOTHING
		RETURNING `+transferColumns,
		id, requestHash, fromWalletID, toWalletID, amount, string(status), failureReason,
	)

	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert transfer claim: %w", err)
	}
	return transfer, nil
}

// FindByID reads a transfer with no lock. It returns (nil, nil) if no
// such transfer exists.
func (r *TransferRepo) FindByID(ctx context.Context, id int64) (*domain.Transfer, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE id = $1`, id)

	transfer, err := scanTransfer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find transfer: %w", err)
	}
	return transfer, nil
}

// InsertLedgerEntries writes the two DEBIT/CREDIT postings for a
// PROCESSED transfer. Entry IDs are DB-generated identity values, not
// app-minted — unlike transfers.id, an entry ID is never a client-facing
// idempotency key or FK target for anything else, so there's nothing to
// gain from hand-rolling it.
func (r *TransferRepo) InsertLedgerEntries(ctx context.Context, tx repository.Tx, transferID int64, fromWalletID, toWalletID string, amount decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES ($1, $2, 'DEBIT', $3, now()), ($1, $4, 'CREDIT', $3, now())
	`, transferID, fromWalletID, amount, toWalletID)
	if err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}
	return nil
}

func scanTransfer(row pgx.Row) (*domain.Transfer, error) {
	var t domain.Transfer
	err := row.Scan(&t.ID, &t.RequestHash, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &t.FailureReason, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
