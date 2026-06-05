package tests

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func createWallet(
	ctx context.Context,
	pool *pgxpool.Pool,
	balance int64,
) uuid.UUID {

	id := uuid.New()

	_, err :=
		pool.Exec(
			ctx,
			`
			INSERT INTO wallets(
				id,
				balance
			)
			VALUES($1,$2)
			`,
			id,
			balance,
		)

	if err != nil {
		panic(err)
	}

	return id
}
