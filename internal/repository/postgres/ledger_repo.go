package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type ledgerRepository struct {
	db DBTX
}

// NewLedgerRepository returns a repository.LedgerRepository backed by PostgreSQL.
func NewLedgerRepository(db DBTX) repository.LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) CreateLedgerEntries(ctx context.Context, entries ...domain.LedgerEntry) error {
	// Strictly enforce the ledger invariant:
	// Either an opening genesis credit entry (len == 1, no transfer ID),
	// or exactly one matching DEBIT and CREDIT pair for a transfer (len == 2).
	if len(entries) == 1 {
		e := entries[0]
		if e.TransferID != "" || e.Type != domain.LedgerEntryTypeCredit || e.Amount <= 0 || e.WalletID == "" {
			return domain.ErrInvalidLedgerPair
		}
	} else if len(entries) == 2 {
		e1, e2 := entries[0], entries[1]
		if e1.TransferID == "" || e1.TransferID != e2.TransferID {
			return domain.ErrInvalidLedgerPair
		}
		if e1.WalletID == "" || e2.WalletID == "" || e1.WalletID == e2.WalletID {
			return domain.ErrInvalidLedgerPair
		}
		if e1.Amount <= 0 || e1.Amount != e2.Amount {
			return domain.ErrInvalidLedgerPair
		}
		isPair := (e1.Type == domain.LedgerEntryTypeDebit && e2.Type == domain.LedgerEntryTypeCredit) ||
			(e1.Type == domain.LedgerEntryTypeCredit && e2.Type == domain.LedgerEntryTypeDebit)
		if !isPair {
			return domain.ErrInvalidLedgerPair
		}
	} else {
		return domain.ErrInvalidLedgerPair
	}

	if len(entries) == 1 {
		entry := entries[0]
		id := entry.ID
		if id == "" {
			id = uuid.NewString()
		}
		now := entry.CreatedAt
		if now.IsZero() {
			now = time.Now().UTC()
		}
		var transferID *string
		if entry.TransferID != "" {
			transferID = &entry.TransferID
		}
		query := `
			INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		if _, err := r.db.Exec(ctx, query, id, transferID, entry.WalletID, string(entry.Type), entry.Amount, now); err != nil {
			return fmt.Errorf("failed to create ledger entry: %w", err)
		}
		return nil
	}

	// len(entries) == 2: insert both entries atomically in a single multi-row statement.
	// This guarantees atomicity even when executed against a connection pool outside an explicit transaction,
	// allowing deferred pair constraint triggers to validate the complete pair.
	e1, e2 := entries[0], entries[1]
	id1, id2 := e1.ID, e2.ID
	if id1 == "" {
		id1 = uuid.NewString()
	}
	if id2 == "" {
		id2 = uuid.NewString()
	}
	now1, now2 := e1.CreatedAt, e2.CreatedAt
	if now1.IsZero() {
		now1 = time.Now().UTC()
	}
	if now2.IsZero() {
		now2 = time.Now().UTC()
	}
	var tID1, tID2 *string
	if e1.TransferID != "" {
		tID1 = &e1.TransferID
	}
	if e2.TransferID != "" {
		tID2 = &e2.TransferID
	}

	query := `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, $6), ($7, $8, $9, $10, $11, $12)
	`
	if _, err := r.db.Exec(ctx, query,
		id1, tID1, e1.WalletID, string(e1.Type), e1.Amount, now1,
		id2, tID2, e2.WalletID, string(e2.Type), e2.Amount, now2,
	); err != nil {
		return fmt.Errorf("failed to create ledger entries: %w", err)
	}
	return nil
}

func (r *ledgerRepository) GetLedgerByWalletID(ctx context.Context, walletID string, limit, offset int) ([]domain.LedgerEntry, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, COALESCE(transfer_id, ''), wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, query, walletID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger entries by wallet: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var typeStr string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typeStr, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Type = domain.LedgerEntryType(typeStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row error reading ledger entries: %w", err)
	}
	return entries, nil
}

func (r *ledgerRepository) GetLedgerByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	query := `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY type ASC
	`
	rows, err := r.db.Query(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger entries by transfer: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var typeStr string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &typeStr, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		e.Type = domain.LedgerEntryType(typeStr)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row error reading ledger entries: %w", err)
	}
	return entries, nil
}

// CalculateBalance computes the derived balance from all historical ledger entries.
func (r *ledgerRepository) CalculateLedgerBalance(ctx context.Context, walletID string) (int64, error) {
	query := `
		SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
		FROM ledger_entries
		WHERE wallet_id = $1
	`
	var balance int64
	if err := r.db.QueryRow(ctx, query, walletID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("failed to calculate balance from ledger: %w", err)
	}
	return balance, nil
}
