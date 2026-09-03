package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

const transferColumns = "id, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at"

func (c *queries) CreateTransfer(ctx context.Context, t *domain.Transfer) error {
	_, err := c.exec(ctx,
		`INSERT INTO transfers (`+transferColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.Status,
		nullString(t.FailureReason), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		if c.db.IsForeignKeyViolation(err) {
			return fmt.Errorf("%w: %s or %s", domain.ErrWalletNotFound, t.FromWalletID, t.ToWalletID)
		}
		return fmt.Errorf("insert transfer %s: %w", t.ID, err)
	}
	return nil
}

func (c *queries) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	t, err := scanTransfer(c.queryRow(ctx, `SELECT `+transferColumns+` FROM transfers WHERE id = ?`, id))
	if err != nil {
		if noRows(err) {
			return nil, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
		}
		return nil, fmt.Errorf("select transfer %s: %w", id, err)
	}
	return t, nil
}

func (t *Tx) LockTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	transfer, err := scanTransfer(t.queryRow(ctx,
		`SELECT `+transferColumns+` FROM transfers WHERE id = ?`+t.db.LockClause(), id))
	if err != nil {
		if noRows(err) {
			return nil, fmt.Errorf("%w: %s", domain.ErrTransferNotFound, id)
		}
		return nil, fmt.Errorf("lock transfer %s: %w", id, err)
	}
	return transfer, nil
}

func (c *queries) SettleTransfer(ctx context.Context, id string, status domain.TransferStatus, failureReason string, now time.Time) (bool, error) {
	res, err := c.exec(ctx,
		`UPDATE transfers SET status = ?, failure_reason = ?, updated_at = ?
		 WHERE id = ? AND status = ?`,
		status, nullString(failureReason), now, id, domain.StatusPending)
	if err != nil {
		return false, fmt.Errorf("settle transfer %s: %w", id, err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("settle transfer %s: %w", id, err)
	}
	return affected == 1, nil
}

func scanTransfer(row interface{ Scan(...any) error }) (*domain.Transfer, error) {
	var (
		t      domain.Transfer
		reason sql.NullString
	)
	if err := row.Scan(&t.ID, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status,
		&reason, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.FailureReason = reason.String
	return &t, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
