package repository

import (
	"database/sql"

	"wallet-transfer-assignment/internal/model"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetWalletForUpdate(tx *sql.Tx, walletID string) (*model.Wallet, error) {
	wallet := &model.Wallet{}

	err := tx.QueryRow(`
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, walletID).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return wallet, nil
}

func (r *Repository) GetTransferByIdempotencyKey(
	tx *sql.Tx,
	key string,
) (*model.Transfer, error) {
	transfer := &model.Transfer{}

	err := tx.QueryRow(`
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
		       amount, status, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, key).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return transfer, nil
}

func (r *Repository) CreateTransfer(
	tx *sql.Tx,
	transfer *model.Transfer,
) error {
	_, err := tx.Exec(`
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)

	return err
}

func (r *Repository) UpdateWalletBalance(
	tx *sql.Tx,
	walletID string,
	balance int64,
) error {
	_, err := tx.Exec(`
		UPDATE wallets
		SET balance = $1, updated_at = NOW()
		WHERE id = $2
	`, balance, walletID)

	return err
}

func (r *Repository) CreateLedgerEntry(
	tx *sql.Tx,
	entry *model.LedgerEntry,
) error {
	_, err := tx.Exec(`
		INSERT INTO ledger_entries (
			transfer_id,
			wallet_id,
			entry_type,
			amount
		)
		VALUES ($1, $2, $3, $4)
	`,
		entry.TransferID,
		entry.WalletID,
		entry.Type,
		entry.Amount,
	)

	return err
}

func (r *Repository) CreateIdempotencyRecord(
	tx *sql.Tx,
	key string,
	transferID string,
	status model.TransferStatus,
) error {
	_, err := tx.Exec(`
		INSERT INTO idempotency_records (
			idempotency_key,
			transfer_id,
			status
		)
		VALUES ($1, $2, $3)
	`,
		key,
		transferID,
		status,
	)

	return err
}

func (r *Repository) LockIdempotencyKey(tx *sql.Tx, key string) error {
	_, err := tx.Exec(`
		SELECT pg_advisory_xact_lock(hashtext($1))
	`, key)

	return err
}
