package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type LedgerRepository interface {
	CreateEntry(
		ctx context.Context,
		tx pgx.Tx,
		entry *domain.LedgerEntry,
	) error
}
