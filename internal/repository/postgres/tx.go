package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type Tx struct {
	tx pgx.Tx
}

func (tx *Tx) InsertIdempotencyRecord(ctx context.Context, key string, fingerprint string) (bool, error) {
	tag, err := tx.tx.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_fingerprint, status)
		VALUES ($1, $2, 'IN_PROGRESS')
		ON CONFLICT (idempotency_key) DO NOTHING
	`, key, fingerprint)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (tx *Tx) GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, error) {
	return scanIdempotencyRecord(tx.tx.QueryRow(ctx, `
		SELECT idempotency_key, request_fingerprint, transfer_id, status,
		       response_snapshot, created_at, updated_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`, key))
}

func (tx *Tx) SetIdempotencyTransfer(ctx context.Context, key string, transferID string) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $2, updated_at = now()
		WHERE idempotency_key = $1
		  AND status = 'IN_PROGRESS'
	`, key, transferID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrRequestInProgress
	}
	return nil
}

func (tx *Tx) CompleteIdempotencyRecord(ctx context.Context, key string, transferID string, responseSnapshot []byte) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $2,
		    status = 'COMPLETED',
		    response_snapshot = $3,
		    updated_at = now()
		WHERE idempotency_key = $1
	`, key, transferID, responseSnapshot)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrRequestInProgress
	}
	return nil
}

func (tx *Tx) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return scanWallet(tx.tx.QueryRow(ctx, `
		SELECT id, owner_id, currency, opening_balance, balance, status, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id))
}

func (tx *Tx) InsertPendingTransfer(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error) {
	return scanTransfer(tx.tx.QueryRow(ctx, `
		INSERT INTO transfers (
			idempotency_key, from_wallet_id, to_wallet_id, amount, currency, status
		)
		VALUES ($1, $2, $3, $4, $5, 'PENDING')
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency,
		          status, failure_reason, created_at, updated_at
	`, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Currency))
}

func (tx *Tx) LockTransferForProcessing(ctx context.Context, id string) (domain.Transfer, error) {
	return scanTransfer(tx.tx.QueryRow(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, currency,
		       status, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = $1
		FOR UPDATE
	`, id))
}

func (tx *Tx) CompleteTransfer(ctx context.Context, transfer domain.Transfer) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE transfers
		SET status = $2,
		    failure_reason = $3,
		    updated_at = now()
		WHERE id = $1
		  AND status = 'PENDING'
	`, transfer.ID, transfer.Status, transfer.FailureReason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidState
	}
	return nil
}

func (tx *Tx) LockWalletsForTransfer(ctx context.Context, fromWalletID string, toWalletID string) (domain.Wallet, domain.Wallet, error) {
	rows, err := tx.tx.Query(ctx, `
		SELECT id, owner_id, currency, opening_balance, balance, status, version, created_at, updated_at
		FROM wallets
		WHERE id = $1 OR id = $2
		ORDER BY id
		FOR UPDATE
	`, fromWalletID, toWalletID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	defer rows.Close()

	wallets := make(map[string]domain.Wallet, 2)
	for rows.Next() {
		wallet, err := scanWallet(rows)
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, err
		}
		wallets[wallet.ID] = wallet
	}
	if err := rows.Err(); err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	fromWallet, found := wallets[fromWalletID]
	if !found {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	toWallet, found := wallets[toWalletID]
	if !found {
		return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
	}
	return fromWallet, toWallet, nil
}

func (tx *Tx) UpdateWalletBalances(ctx context.Context, fromWallet domain.Wallet, toWallet domain.Wallet) error {
	tag, err := tx.tx.Exec(ctx, `
		UPDATE wallets
		SET balance = CASE
		        WHEN id = $1 THEN $2::bigint
		        WHEN id = $3 THEN $4::bigint
		    END,
		    version = version + 1,
		    updated_at = now()
		WHERE id IN ($1, $3)
	`, fromWallet.ID, fromWallet.Balance, toWallet.ID, toWallet.Balance)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 2 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (tx *Tx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, entry := range entries {
		batch.Queue(`
			INSERT INTO ledger_entries (
				transfer_id, wallet_id, entry_type, amount, balance_after
			)
			VALUES ($1, $2, $3, $4, $5)
		`, entry.TransferID, entry.WalletID, entry.Type, entry.Amount, entry.BalanceAfter)
	}
	results := tx.tx.SendBatch(ctx, batch)
	defer results.Close()

	for range entries {
		if _, err := results.Exec(); err != nil {
			return err
		}
	}
	return nil
}
