package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ConnectPostgres(
	ctx context.Context,
	databaseURL string,
) *pgxpool.Pool {

	db, err := pgxpool.New(
		ctx,
		databaseURL,
	)

	if err != nil {
		log.Fatalf(
			"failed to create postgres pool: %v",
			err,
		)
	}

	if err := db.Ping(ctx); err != nil {
		log.Fatalf(
			"failed to connect to postgres: %v",
			err,
		)
	}

	fmt.Println("connected to PostgreSQL")

	return db
}
