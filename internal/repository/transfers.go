package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
)

const transferSelect = `
	SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	FROM transfers`

func (t *Tx) InsertTransfer(ctx context.Context, tr domain.Transfer) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tr.ID,
		tr.IdempotencyKey,
		tr.FromWalletID,
		tr.ToWalletID,
		tr.Amount,
		string(tr.Status),
		tr.FailureReason,
		tr.CreatedAt.UTC().Format(timeLayout),
		tr.UpdatedAt.UTC().Format(timeLayout),
	)
	return err
}

func (t *Tx) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	return scanTransfer(t.tx.QueryRowContext(ctx, transferSelect+" WHERE id = ?", id))
}

func (t *Tx) InsertLedgerEntry(ctx context.Context, e domain.LedgerEntry) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount, e.CreatedAt.UTC().Format(timeLayout),
	)
	return err
}

func (t *Tx) InsertIdempotency(ctx context.Context, rec domain.IdempotencyRecord) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, transfer_id, request_hash, created_at)
		VALUES (?, ?, ?, ?)`,
		rec.IdempotencyKey, rec.TransferID, rec.RequestHash, rec.CreatedAt.UTC().Format(timeLayout),
	)
	return err
}

func (t *Tx) GetIdempotency(ctx context.Context, key string) (domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	var created string
	err := t.tx.QueryRowContext(ctx, `
		SELECT idempotency_key, transfer_id, request_hash, created_at
		FROM idempotency_records
		WHERE idempotency_key = ?`, key,
	).Scan(&rec.IdempotencyKey, &rec.TransferID, &rec.RequestHash, &created)
	if err != nil {
		return domain.IdempotencyRecord{}, mapNotFound(err, ErrNotFound)
	}
	rec.CreatedAt, err = time.Parse(timeLayout, created)
	if err != nil {
		return domain.IdempotencyRecord{}, err
	}
	return rec, nil
}

func scanTransfer(row *sql.Row) (domain.Transfer, error) {
	var tr domain.Transfer
	var status string
	var created, updated string
	err := row.Scan(
		&tr.ID,
		&tr.IdempotencyKey,
		&tr.FromWalletID,
		&tr.ToWalletID,
		&tr.Amount,
		&status,
		&tr.FailureReason,
		&created,
		&updated,
	)
	if err != nil {
		return domain.Transfer{}, mapNotFound(err, domain.ErrTransferNotFound)
	}
	tr.Status = domain.TransferStatus(status)
	tr.CreatedAt, err = time.Parse(timeLayout, created)
	if err != nil {
		return domain.Transfer{}, err
	}
	tr.UpdatedAt, err = time.Parse(timeLayout, updated)
	if err != nil {
		return domain.Transfer{}, err
	}
	return tr, nil
}
