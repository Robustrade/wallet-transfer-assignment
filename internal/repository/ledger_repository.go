package repository

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
)

type LedgerRepository interface {
	CreateMany(
		ctx context.Context,
		entries ...*domain.LedgerEntry,
	) error
}
