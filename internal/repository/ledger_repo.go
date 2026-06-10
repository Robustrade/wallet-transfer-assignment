package repository

import (
	"context"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// LedgerRepository handles double-entry ledger persistence.
type LedgerRepository struct{}

// NewLedgerRepository creates a new LedgerRepository.
func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

// CreatePair atomically creates a DEBIT and CREDIT ledger entry pair for a transfer.
// This enforces double-entry bookkeeping: every successful (PROCESSED) transfer generates exactly two entries
// that balance each other (debit amount == credit amount).
func (r *LedgerRepository) CreatePair(ctx context.Context, db DBTX, transferID, fromWalletID, toWalletID string, amount int64) error {
	// DEBIT entry — source wallet
	_, err := db.Exec(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		 VALUES ($1::uuid, $2, $3, $4)`,
		transferID, fromWalletID, domain.EntryTypeDebit, amount,
	)
	if err != nil {
		return err
	}

	// CREDIT entry — destination wallet
	_, err = db.Exec(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		 VALUES ($1::uuid, $2, $3, $4)`,
		transferID, toWalletID, domain.EntryTypeCredit, amount,
	)
	return err
}

// GetByTransferID retrieves all ledger entries for a given transfer.
func (r *LedgerRepository) GetByTransferID(ctx context.Context, db DBTX, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, transfer_id::text, wallet_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE transfer_id = $1::uuid
		 ORDER BY entry_type`,
		transferID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetByWalletID retrieves all ledger entries for a given wallet, ordered by most recent first.
func (r *LedgerRepository) GetByWalletID(ctx context.Context, db DBTX, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := db.Query(ctx,
		`SELECT id::text, transfer_id::text, wallet_id, entry_type, amount, created_at
		 FROM ledger_entries WHERE wallet_id = $1
		 ORDER BY created_at DESC`,
		walletID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
