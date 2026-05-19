package cmd

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dependencies struct {
	Database *pgxpool.Pool
}

func InitDependencies(ctx context.Context, config *Config) (*dependencies, error) {
	deps := &dependencies{}
	var err error

	if deps.Database, err = initDatabase(ctx, config); err != nil {
		return nil, err
	}

	return deps, nil
}

func (deps *dependencies) Close() {
	if deps.Database != nil {
		deps.Database.Close()
	}
}

func initDatabase(ctx context.Context, config *Config) (*pgxpool.Pool, error) {
	dbConfig, err := pgxpool.ParseConfig(config.DATABASE_URL)
	if err != nil {
		return nil, fmt.Errorf("unable parse database url: %v", err)
	}

	dbConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		return nil, fmt.Errorf("unable initialise database: %v", err)
	}

	return pool, nil
}
