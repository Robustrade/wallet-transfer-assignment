package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skc1234/wallet-transfer-assignment/internal/domain"
)

type TransferRepository struct {
	DB *pgxpool.Pool
}

func NewTransferRepository(db *pgxpool.Pool) *TransferRepository {
	return &TransferRepository{
		DB: db,
	}
}

// CreateTransfer transfers money atomically between two wallets.
// Amount is provided in paise: Rs. 200 = 20000.
func (r *TransferRepository) CreateTransfer(
	ctx context.Context,
	idempotencyKey string,
	fromWalletID string,
	toWalletID string,
	amount int64,
) (domain.Transfer, error) {

	var transfer domain.Transfer

	idempotencyKey = strings.TrimSpace(idempotencyKey)

	if idempotencyKey == "" {
		return transfer, fmt.Errorf("idempotency key is required")
	}

	if amount <= 0 {
		return transfer, fmt.Errorf("amount must be greater than zero")
	}

	if fromWalletID == toWalletID {
		return transfer, fmt.Errorf("sender and receiver cannot be the same")
	}

	// Generate a hash of the request payload.
	requestData := fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)
	requestHash := fmt.Sprintf("%x", sha256.Sum256([]byte(requestData)))

	// Convert paise into an exact decimal string for PostgreSQL NUMERIC.
	amountDecimal := fmt.Sprintf("%d.%02d", amount/100, amount%100)

	// Start database transaction.
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return transfer, err
	}
	defer tx.Rollback(ctx)

	// Register this idempotency key.
	// A duplicate key waits for the first transaction to finish.
	result, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (
			idempotency_key,
			request_hash
		)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, idempotencyKey, requestHash)

	if err != nil {
		return transfer, err
	}

	// If this key already exists, return its previous transfer.
	if result.RowsAffected() == 0 {
		var existingHash string
		var existingTransferID pgtype.Text

		err := tx.QueryRow(ctx, `
			SELECT request_hash, transfer_id::text
			FROM idempotency_records
			WHERE idempotency_key = $1
		`, idempotencyKey).Scan(
			&existingHash,
			&existingTransferID,
		)
		if err != nil {
			return transfer, err
		}

		// Same key cannot be reused for a different request.
		if existingHash != requestHash {
			return transfer, fmt.Errorf(
				"idempotency key already used with a different request",
			)
		}

		if !existingTransferID.Valid {
			return transfer, fmt.Errorf(
				"previous transfer is not available for this idempotency key",
			)
		}

		err = tx.QueryRow(ctx, `
			SELECT id::text, from_wallet_id::text,
			       to_wallet_id::text, status, created_at
			FROM transfers
			WHERE id = $1
		`, existingTransferID.String).Scan(
			&transfer.ID,
			&transfer.FromWalletID,
			&transfer.ToWalletID,
			&transfer.Status,
			&transfer.CreatedAt,
		)
		if err != nil {
			return transfer, err
		}

		transfer.Amount = amount

		if err := tx.Commit(ctx); err != nil {
			return domain.Transfer{}, err
		}

		return transfer, nil
	}

	// Lock both wallets in a consistent order.
	rows, err := tx.Query(ctx, `
		SELECT id::text
		FROM wallets
		WHERE id IN ($1, $2)
		ORDER BY id
		FOR UPDATE
	`, fromWalletID, toWalletID)
	if err != nil {
		return transfer, err
	}

	foundWallets := make(map[string]bool)

	for rows.Next() {
		var walletID string

		if err := rows.Scan(&walletID); err != nil {
			rows.Close()
			return transfer, err
		}

		foundWallets[walletID] = true
	}

	rowsErr := rows.Err()
	rows.Close()

	if rowsErr != nil {
		return transfer, rowsErr
	}

	if !foundWallets[fromWalletID] || !foundWallets[toWalletID] {
		return transfer, fmt.Errorf("sender or receiver wallet not found")
	}

	// Debit sender only if sufficient balance is available.
	result, err = tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance - $1::numeric,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		  AND balance >= $1::numeric
	`, amountDecimal, fromWalletID)
	if err != nil {
		return transfer, err
	}

	if result.RowsAffected() != 1 {
		return transfer, fmt.Errorf("insufficient wallet balance")
	}

	// Credit receiver.
	_, err = tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $1::numeric,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, amountDecimal, toWalletID)
	if err != nil {
		return transfer, err
	}

	// Create transfer record.
	err = tx.QueryRow(ctx, `
		INSERT INTO transfers (
			id,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES (
			gen_random_uuid(),
			$1,
			$2,
			$3::numeric,
			'PROCESSED'
		)
		RETURNING id::text, created_at
	`, fromWalletID, toWalletID, amountDecimal).Scan(
		&transfer.ID,
		&transfer.CreatedAt,
	)
	if err != nil {
		return transfer, err
	}

	// Create sender DEBIT ledger entry.
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transfer_id,
			entry_type,
			amount
		)
		VALUES (
			gen_random_uuid(),
			$1,
			$2,
			'DEBIT',
			$3::numeric
		)
	`, fromWalletID, transfer.ID, amountDecimal)
	if err != nil {
		return transfer, err
	}

	// Create receiver CREDIT ledger entry.
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transfer_id,
			entry_type,
			amount
		)
		VALUES (
			gen_random_uuid(),
			$1,
			$2,
			'CREDIT',
			$3::numeric
		)
	`, toWalletID, transfer.ID, amountDecimal)
	if err != nil {
		return transfer, err
	}

	// Link the idempotency key to the completed transfer.
	_, err = tx.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $1
		WHERE idempotency_key = $2
	`, transfer.ID, idempotencyKey)
	if err != nil {
		return transfer, err
	}

	// Commit all database changes together.
	if err := tx.Commit(ctx); err != nil {
		return domain.Transfer{}, err
	}

	transfer.FromWalletID = fromWalletID
	transfer.ToWalletID = toWalletID
	transfer.Amount = amount
	transfer.Status = domain.TransferProcessed

	return transfer, nil
}
