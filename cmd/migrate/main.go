// Command migrate applies the embedded schema migrations to DATABASE_URL.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"

	"wallet/internal/config"
	"wallet/internal/constants"
	"wallet/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	connectCtx, cancel := context.WithTimeout(ctx, constants.ConnectTimeout)
	conn, err := pgx.Connect(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))

	applied, err := migrations.Apply(ctx, conn)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		logger.Info("migrations up to date")
		return nil
	}
	for _, v := range applied {
		logger.Info("migration applied", "version", v)
	}
	return nil
}
