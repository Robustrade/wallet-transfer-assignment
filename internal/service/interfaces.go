package service

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// DB is the subset of pgxpool.Pool the service needs.
type DB interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)

	repository.Querier
}
