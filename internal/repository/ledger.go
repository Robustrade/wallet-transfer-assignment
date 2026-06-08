package repository

import (
	"context"
	"fmt"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type InsertLedgerEntryParams struct {
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       domain.LedgerEntryType
	Amount     int64
}

func InsertLedgerEntry(ctx context.Context, tx pgx.Tx, params InsertLedgerEntryParams) (*domain.LedgerEntry, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4)
		RETURNING id, transfer_id, wallet_id, type, amount, created_at
	`, params.TransferID, params.WalletID, params.Type, params.Amount)

	entry, err := scanLedgerEntry(row)
	if err != nil {
		return nil, fmt.Errorf("insert ledger entry: %w", err)
	}
	return entry, nil
}

func ListLedgerEntriesByTransferID(ctx context.Context, q pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	rows, err := q.Query(ctx, `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at ASC, CASE type WHEN 'DEBIT' THEN 0 ELSE 1 END
	`, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		entry, err := scanLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func scanLedgerEntry(row scannable) (*domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	var entryType string
	err := row.Scan(
		&entry.ID,
		&entry.TransferID,
		&entry.WalletID,
		&entryType,
		&entry.Amount,
		&entry.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	entry.Type, err = domain.ParseLedgerEntryType(entryType)
	if err != nil {
		return nil, err
	}

	return &entry, nil
}
