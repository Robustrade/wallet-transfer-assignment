package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/db"
)

type LedgerRepository interface {
	CreateLedgerEntry(ctx context.Context, entry domain.LedgerEntry) (*domain.LedgerEntry, error)
	GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

type PostgresLedgerRepository struct {
	pool *pgxpool.Pool
}

func NewLedgerRepository(pool *pgxpool.Pool) *PostgresLedgerRepository {
	return &PostgresLedgerRepository{
		pool: pool,
	}
}

func (r *PostgresLedgerRepository) q(ctx context.Context) *db.Queries {
	return db.New(getDBTX(ctx, r.pool))
}

func (r *PostgresLedgerRepository) CreateLedgerEntry(ctx context.Context, entry domain.LedgerEntry) (*domain.LedgerEntry, error) {
	row, err := r.q(ctx).CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
		ID:         entry.ID,
		TransferID: entry.TransferID,
		WalletID:   entry.WalletID,
		Type:       string(entry.Type),
		Amount:     entry.Amount,
	})
	if err != nil {
		return nil, err
	}
	return toDomainLedgerEntry(row), nil
}

func (r *PostgresLedgerRepository) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.q(ctx).GetLedgerEntriesByTransferID(ctx, transferID)
	if err != nil {
		return nil, err
	}
	entries := make([]domain.LedgerEntry, len(rows))
	for i, row := range rows {
		entries[i] = *toDomainLedgerEntry(row)
	}
	return entries, nil
}

func toDomainLedgerEntry(row db.LedgerEntry) *domain.LedgerEntry {
	return &domain.LedgerEntry{
		ID:         row.ID,
		TransferID: row.TransferID,
		WalletID:   row.WalletID,
		Type:       domain.LedgerEntryType(row.Type),
		Amount:     row.Amount,
		CreatedAt:  row.CreatedAt.Time,
	}
}
