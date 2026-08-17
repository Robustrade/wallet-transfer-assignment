package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connection(dbUrl string) (*pgxpool.Pool, error) {
	ctx := context.Background()
	// connStr := "postgres://postgres:postgres@172.18.16.1:5432/wallet?sslmode=disable&search_path=wallet"

	config, err := pgxpool.ParseConfig(dbUrl)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse config: %v", err)
	}

	// can configured in .env file
	config.MaxConns = 20
	config.MinConns = 5
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("Failed to create database pool: %w", err)
	}
	// defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("Failed to ping db: %w", err)
	}
	return pool, nil
}
