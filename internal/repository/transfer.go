package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
)

var ErrTransferNotFound = errors.New("transfer not found")

type TransferStore interface {
	Create(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error)
	GetByID(ctx context.Context, id string) (domain.Transfer, error)
	MarkProcessed(ctx context.Context, id string) (domain.Transfer, error)
	MarkFailed(ctx context.Context, id, failureCode string) (domain.Transfer, error)
}

// TransferRepository must share the caller's *sql.Tx with wallet and ledger
// repositories when these writes belong to one atomic transfer operation.
type TransferRepository struct{ db DBTX }

func NewTransferRepository(db DBTX) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) Create(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error) {
	if err := transfer.Validate(); err != nil {
		return domain.Transfer{}, fmt.Errorf("validate transfer: %w", err)
	}
	if transfer.Status != domain.TransferPending {
		return domain.Transfer{}, fmt.Errorf("new transfer must be pending: %w", domain.ErrInvalidTransferStatus)
	}
	const query = `
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, failure_code)
		VALUES ($1, $2, $3, $4, $5, NULL)
		RETURNING id, from_wallet_id, to_wallet_id, amount, status, failure_code`
	created, err := scanTransfer(r.db.QueryRowContext(ctx, query,
		transfer.ID, transfer.FromWalletID, transfer.ToWalletID, int64(transfer.Amount), string(transfer.Status)))
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("create transfer: %w", err)
	}
	return created, nil
}

func (r *TransferRepository) GetByID(ctx context.Context, id string) (domain.Transfer, error) {
	if id == "" {
		return domain.Transfer{}, domain.ErrInvalidTransferID
	}
	const query = `
		SELECT id, from_wallet_id, to_wallet_id, amount, status, failure_code
		FROM transfers WHERE id = $1`
	transfer, err := scanTransfer(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, fmt.Errorf("%w: %s", ErrTransferNotFound, id)
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer %q: %w", id, err)
	}
	return transfer, nil
}

// MarkProcessed allows only PENDING -> PROCESSED.
func (r *TransferRepository) MarkProcessed(ctx context.Context, id string) (domain.Transfer, error) {
	if id == "" {
		return domain.Transfer{}, domain.ErrInvalidTransferID
	}
	const query = `
		UPDATE transfers
		SET status = 'PROCESSED', failure_code = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'PENDING'
		RETURNING id, from_wallet_id, to_wallet_id, amount, status, failure_code`
	return r.updatePending(ctx, id, query)
}

// MarkFailed allows only PENDING -> FAILED and requires a nonblank failure code.
func (r *TransferRepository) MarkFailed(ctx context.Context, id, failureCode string) (domain.Transfer, error) {
	if id == "" {
		return domain.Transfer{}, domain.ErrInvalidTransferID
	}
	if strings.TrimSpace(failureCode) == "" {
		return domain.Transfer{}, domain.ErrInvalidFailureCode
	}
	const query = `
		UPDATE transfers
		SET status = 'FAILED', failure_code = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'PENDING'
		RETURNING id, from_wallet_id, to_wallet_id, amount, status, failure_code`
	transfer, err := scanTransfer(r.db.QueryRowContext(ctx, query, id, failureCode))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, r.classifyFailedTransition(ctx, id)
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("mark transfer %q failed: %w", id, err)
	}
	return transfer, nil
}

func (r *TransferRepository) updatePending(ctx context.Context, id, query string) (domain.Transfer, error) {
	transfer, err := scanTransfer(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, r.classifyFailedTransition(ctx, id)
	}
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("update transfer %q: %w", id, err)
	}
	return transfer, nil
}

func (r *TransferRepository) classifyFailedTransition(ctx context.Context, id string) error {
	_, err := r.GetByID(ctx, id)
	if errors.Is(err, ErrTransferNotFound) {
		return fmt.Errorf("%w: %s", ErrTransferNotFound, id)
	}
	if err != nil {
		return err
	}
	return domain.ErrInvalidTransferTransition
}

func scanTransfer(row *sql.Row) (domain.Transfer, error) {
	var transfer domain.Transfer
	var amount int64
	var status string
	var failureCode sql.NullString
	if err := row.Scan(&transfer.ID, &transfer.FromWalletID, &transfer.ToWalletID, &amount, &status, &failureCode); err != nil {
		return domain.Transfer{}, err
	}
	transfer.Amount = domain.Amount(amount)
	transfer.Status = domain.TransferStatus(status)
	if failureCode.Valid {
		transfer.FailureCode = failureCode.String
	}
	if err := transfer.Validate(); err != nil {
		return domain.Transfer{}, fmt.Errorf("invalid transfer row: %w", err)
	}
	return transfer, nil
}
