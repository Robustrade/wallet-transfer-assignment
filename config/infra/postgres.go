// Package infra holds initializers for shared infrastructure clients.
package infra

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgresConfig holds the connection pool tuning parameters.
type PostgresConfig struct {
	DSN             string        // postgres://user:pass@host:5432/db?sslmode=disable
	MaxOpenConns    int           // default 10
	MaxIdleConns    int           // default 5
	ConnMaxLifetime time.Duration // default 30m
	PingTimeout     time.Duration // default 5s
}

// InitPostgres opens a GORM-managed Postgres pool, configures the underlying
// *sql.DB, and verifies connectivity with a short Ping.
//
// We use GORM as the connection manager and as the executor for raw SQL
// (`tx.Raw(...).Row().Scan(...)`, `tx.Exec(...)`); we deliberately do NOT
// use GORM's ORM/query-builder DSL — every statement in `repo/postgres/*` is
// hand-written parameterized SQL, so the schema is owned by the migrations,
// not by reflection over Go structs.
func InitPostgres(ctx context.Context, cfg PostgresConfig) (*gorm.DB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres: empty DSN")
	}

	gormCfg := &gorm.Config{
		// We never use GORM's ORM, so we don't want it scanning our raw-SQL
		// outputs and complaining about unmapped columns; Silent quiets it.
		Logger: logger.Default.LogMode(logger.Silent),
		// Skip default tx wrapping on every Exec — we manage transactions
		// explicitly in tx_manager.go.
		SkipDefaultTransaction: true,
		// Don't try to translate driver errors into ErrRecordNotFound etc.;
		// we want the raw pgx errors so SQLSTATE checks (FK / CHECK
		// violations) keep working.
		TranslateError: false,
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres: underlying sql.DB: %w", err)
	}

	if cfg.MaxOpenConns <= 0 {
		cfg.MaxOpenConns = 10
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 5
	}
	if cfg.ConnMaxLifetime <= 0 {
		cfg.ConnMaxLifetime = 30 * time.Minute
	}
	if cfg.PingTimeout <= 0 {
		cfg.PingTimeout = 5 * time.Second
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.PingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return db, nil
}

// ClosePostgres releases the underlying *sql.DB. Use on graceful shutdown.
func ClosePostgres(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
