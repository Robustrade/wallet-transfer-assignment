package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(connString string) (*pgxpool.Pool, error) {

	return pgxpool.New(
		context.Background(),
		connString,
	)
}
