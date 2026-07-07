package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
)

type transaction struct {
	tx pgx.Tx
}

func (t *transaction) TryCreateIdempotency(
	ctx context.Context,
	key string,
	requestHash string,
) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, key, requestHash)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (t *transaction) FindTransferByIdempotencyKey(
	ctx context.Context,
	key string,
) (*domain.IdempotencyRecord, *domain.Transfer, error) {
	const query = `
		SELECT
			i.idempotency_key,
			i.request_hash,
			i.transfer_id,
			t.id,
			t.idempotency_key,
			t.from_wallet_id,
			t.to_wallet_id,
			t.amount,
			t.status,
			COALESCE(t.failure_code, ''),
			t.created_at,
			t.updated_at
		FROM idempotency_records i
		JOIN transfers t ON t.id = i.transfer_id
		WHERE i.idempotency_key = $1
	`

	record := &domain.IdempotencyRecord{}
	transfer := &domain.Transfer{}
	err := t.tx.QueryRow(ctx, query, key).Scan(
		&record.Key,
		&record.RequestHash,
		&record.TransferID,
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.FailureCode,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if err != nil {
		return nil, nil, err
	}
	return record, transfer, nil
}

func (t *transaction) LinkIdempotencyToTransfer(
	ctx context.Context,
	key string,
	transferID string,
) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $2
		WHERE idempotency_key = $1 AND transfer_id IS NULL
	`, key, transferID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("idempotency record was not linked")
	}
	return nil
}

func (t *transaction) CompleteIdempotency(ctx context.Context, key string) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE idempotency_records
		SET completed_at = now()
		WHERE idempotency_key = $1
			AND transfer_id IS NOT NULL
			AND completed_at IS NULL
	`, key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("idempotency record was not completed")
	}
	return nil
}

func (t *transaction) CreateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			failure_code,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
	`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
		transfer.FailureCode,
		transfer.CreatedAt,
		transfer.UpdatedAt,
	)
	return err
}

func (t *transaction) UpdateTransfer(ctx context.Context, transfer *domain.Transfer) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE transfers
		SET status = $2, failure_code = NULLIF($3, ''), updated_at = $4
		WHERE id = $1 AND status = 'PENDING'
	`,
		transfer.ID,
		transfer.Status,
		transfer.FailureCode,
		transfer.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidTransition
	}
	return nil
}

func (t *transaction) LockWallets(
	ctx context.Context,
	walletIDs []string,
) ([]domain.Wallet, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, balance
		FROM wallets
		WHERE id = ANY($1::text[])
		ORDER BY id
		FOR UPDATE
	`, walletIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	wallets := make([]domain.Wallet, 0, len(walletIDs))
	for rows.Next() {
		var wallet domain.Wallet
		if err := rows.Scan(&wallet.ID, &wallet.Balance); err != nil {
			return nil, err
		}
		wallets = append(wallets, wallet)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return wallets, nil
}

func (t *transaction) UpdateWalletBalance(
	ctx context.Context,
	walletID string,
	balance int64,
) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1
	`, walletID, balance)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("wallet %q was not updated", walletID)
	}
	return nil
}

func (t *transaction) CreateLedgerEntries(
	ctx context.Context,
	entries []domain.LedgerEntry,
) error {
	batch := &pgx.Batch{}
	for _, entry := range entries {
		batch.Queue(`
			INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount)
			VALUES ($1, $2, $3, $4, $5)
		`, entry.ID, entry.WalletID, entry.TransferID, entry.Type, entry.Amount)
	}

	results := t.tx.SendBatch(ctx, batch)
	for range entries {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	return results.Close()
}
