package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type LedgerRepository interface {
	Create(
		ctx context.Context,
		tx pgx.Tx,
		entry *domain.LedgerEntry,
	) error
}
