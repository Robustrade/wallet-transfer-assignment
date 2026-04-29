package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wallet-transfer/internal/transfer"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/logger"
)

// TransferRepository defines persistence operations for transfers and ledger entries.
type TransferRepository interface {
	// Transfer operations
	CreateTransfer(ctx context.Context, db database.DBTX, t *transfer.Transfer) error
	GetTransferByID(ctx context.Context, db database.DBTX, id string) (*transfer.Transfer, error)
	GetTransferByIdempotencyKey(ctx context.Context, db database.DBTX, key string) (*transfer.Transfer, error)
	UpdateTransferStatus(ctx context.Context, db database.DBTX, id, status, failureReason, updatedBy string) error
	GetAllTransfers(ctx context.Context, db database.DBTX) ([]*transfer.Transfer, error)

	// Ledger operations
	CreateLedgerEntry(ctx context.Context, db database.DBTX, e *transfer.LedgerEntry) error
	GetLedgerEntriesByTransferID(ctx context.Context, db database.DBTX, transferID string) ([]*transfer.LedgerEntry, error)
	GetLedgerEntriesByWalletID(ctx context.Context, db database.DBTX, walletID string) ([]*transfer.LedgerEntry, error)
}

type transferRepository struct{}

// NewTransferRepository creates a new TransferRepository.
func NewTransferRepository() TransferRepository {
	return &transferRepository{}
}

// ---- Transfer operations ----

func (r *transferRepository) CreateTransfer(ctx context.Context, db database.DBTX, t *transfer.Transfer) error {
	const fn = "transferRepository.CreateTransfer"
	sql := `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	now := time.Now().UTC()
	_, err := db.Exec(ctx, sql,
		t.ID, nullableString(t.IdempotencyKey), t.FromWalletID, t.ToWalletID,
		t.Amount, t.Status, t.AuditLog.CreatedBy, t.AuditLog.UpdatedBy, now, now,
	)
	if err != nil {
		logger.Error(fn, "insert transfer failed", err, map[string]interface{}{"transfer_id": t.ID})
		return err
	}
	t.AuditLog.CreatedAt = now
	t.AuditLog.UpdatedAt = now
	return nil
}

func (r *transferRepository) GetTransferByID(ctx context.Context, db database.DBTX, id string) (*transfer.Transfer, error) {
	const fn = "transferRepository.GetTransferByID"
	sql := `SELECT id, COALESCE(idempotency_key,''), from_wallet_id, to_wallet_id, amount, status,
	               COALESCE(failure_reason,''), created_by, updated_by, created_at, updated_at
	        FROM transfers WHERE id = $1`
	row := db.QueryRow(ctx, sql, id)
	t, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		logger.Error(fn, "query transfer failed", err, map[string]interface{}{"transfer_id": id})
		return nil, err
	}
	return t, nil
}

func (r *transferRepository) GetTransferByIdempotencyKey(ctx context.Context, db database.DBTX, key string) (*transfer.Transfer, error) {
	const fn = "transferRepository.GetTransferByIdempotencyKey"
	sql := `SELECT id, COALESCE(idempotency_key,''), from_wallet_id, to_wallet_id, amount, status,
	               COALESCE(failure_reason,''), created_by, updated_by, created_at, updated_at
	        FROM transfers WHERE idempotency_key = $1`
	row := db.QueryRow(ctx, sql, key)
	t, err := scanTransfer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		logger.Error(fn, "query by idempotency key failed", err, map[string]interface{}{"key": key})
		return nil, err
	}
	return t, nil
}

func (r *transferRepository) UpdateTransferStatus(ctx context.Context, db database.DBTX, id, status, failureReason, updatedBy string) error {
	const fn = "transferRepository.UpdateTransferStatus"
	sql := `UPDATE transfers SET status = $1, failure_reason = $2, updated_by = $3, updated_at = $4 WHERE id = $5`
	_, err := db.Exec(ctx, sql, status, nullableString(failureReason), updatedBy, time.Now().UTC(), id)
	if err != nil {
		logger.Error(fn, "update transfer status failed", err, map[string]interface{}{"transfer_id": id, "status": status})
	}
	return err
}

func (r *transferRepository) GetAllTransfers(ctx context.Context, db database.DBTX) ([]*transfer.Transfer, error) {
	const fn = "transferRepository.GetAllTransfers"
	sql := `SELECT id, COALESCE(idempotency_key,''), from_wallet_id, to_wallet_id, amount, status,
	               COALESCE(failure_reason,''), created_by, updated_by, created_at, updated_at
	        FROM transfers ORDER BY created_at DESC`
	rows, err := db.Query(ctx, sql)
	if err != nil {
		logger.Error(fn, "query all transfers failed", err, nil)
		return nil, err
	}
	defer rows.Close()

	var transfers []*transfer.Transfer
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		transfers = append(transfers, t)
	}
	return transfers, rows.Err()
}

// ---- Ledger operations ----

func (r *transferRepository) CreateLedgerEntry(ctx context.Context, db database.DBTX, e *transfer.LedgerEntry) error {
	const fn = "transferRepository.CreateLedgerEntry"
	sql := `INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	now := time.Now().UTC()
	_, err := db.Exec(ctx, sql, e.ID, e.WalletID, e.TransferID, e.Type, e.Amount, e.CreatedBy, now)
	if err != nil {
		logger.Error(fn, "insert ledger entry failed", err, map[string]interface{}{"entry_id": e.ID})
		return err
	}
	e.CreatedAt = now
	return nil
}

func (r *transferRepository) GetLedgerEntriesByTransferID(ctx context.Context, db database.DBTX, transferID string) ([]*transfer.LedgerEntry, error) {
	const fn = "transferRepository.GetLedgerEntriesByTransferID"
	sql := `SELECT id, wallet_id, transfer_id, type, amount, created_by, created_at FROM ledger_entries WHERE transfer_id = $1 ORDER BY created_at`
	return r.queryLedger(ctx, fn, db, sql, transferID)
}

func (r *transferRepository) GetLedgerEntriesByWalletID(ctx context.Context, db database.DBTX, walletID string) ([]*transfer.LedgerEntry, error) {
	const fn = "transferRepository.GetLedgerEntriesByWalletID"
	sql := `SELECT id, wallet_id, transfer_id, type, amount, created_by, created_at FROM ledger_entries WHERE wallet_id = $1 ORDER BY created_at DESC`
	return r.queryLedger(ctx, fn, db, sql, walletID)
}

func (r *transferRepository) queryLedger(ctx context.Context, fn string, db database.DBTX, sql string, arg string) ([]*transfer.LedgerEntry, error) {
	rows, err := db.Query(ctx, sql, arg)
	if err != nil {
		logger.Error(fn, "query ledger failed", err, nil)
		return nil, err
	}
	defer rows.Close()

	var entries []*transfer.LedgerEntry
	for rows.Next() {
		e, err := scanLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ---- Helpers ----

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransfer(row rowScanner) (*transfer.Transfer, error) {
	var t transfer.Transfer
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID,
		&t.Amount, &t.Status, &t.FailureReason,
		&t.AuditLog.CreatedBy, &t.AuditLog.UpdatedBy, &t.AuditLog.CreatedAt, &t.AuditLog.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanLedgerEntry(row rowScanner) (*transfer.LedgerEntry, error) {
	var e transfer.LedgerEntry
	err := row.Scan(&e.ID, &e.WalletID, &e.TransferID, &e.Type, &e.Amount, &e.CreatedBy, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
