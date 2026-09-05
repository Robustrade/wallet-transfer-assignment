package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	"wallet-transfer/internal/config"
	"wallet-transfer/internal/repository/postgres"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "only apply migrations without seeding")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.PoolConfig{
		MaxOpenConns: 5,
		MaxIdleConns: 5,
	})
	if err != nil {
		slog.Error("failed to connect to db", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	applied, err := postgres.Migrate(ctx, db, cfg.MigrationsDir)
	if err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations up to date", "applied", len(applied))

	if *migrateOnly {
		return
	}

	if err := postgres.NewSeeder(db).Seed(ctx); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}
	slog.Info("successfully seeded demo wallets")
}
