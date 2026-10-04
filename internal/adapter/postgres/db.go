package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the pgx database/sql driver used by goose
	"github.com/pressly/goose/v3"

	"github.com/amantiwari/wallet-transfer/internal/adapter/postgres/migrations"
	"github.com/amantiwari/wallet-transfer/internal/app/port"
)

// OpenConfig is the connection and transaction configuration for the pool.
type OpenConfig struct {
	DSN                  string
	MaxConns             int32
	MinConns             int32
	MaxConnLifetime      time.Duration
	MaxConnIdleTime      time.Duration
	ConnectRetries       int
	ConnectRetryInterval time.Duration
	LockTimeout          time.Duration
	StatementTimeout     time.Duration
}

// DB is the PostgreSQL adapter. It implements the unit of work and the read ports.
type DB struct {
	pool             *pgxpool.Pool
	lockTimeout      time.Duration
	statementTimeout time.Duration
}

// Open connects and pings PostgreSQL, retrying while the server starts.
func Open(ctx context.Context, cfg OpenConfig, log *slog.Logger) (*DB, error) {
	if log == nil {
		log = slog.Default()
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["application_name"] = "wallet-transfer"

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	retries := cfg.ConnectRetries
	if retries < 1 {
		retries = 1
	}
	interval := cfg.ConnectRetryInterval
	if interval <= 0 {
		interval = time.Second
	}
	var last error
	for attempt := 1; attempt <= retries; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		last = pool.Ping(pingCtx)
		cancel()
		if last == nil {
			return &DB{
				pool:             pool,
				lockTimeout:      cfg.LockTimeout,
				statementTimeout: cfg.StatementTimeout,
			}, nil
		}
		log.Warn("database not ready", "attempt", attempt, "error", last.Error())
		if attempt == retries {
			break
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("connect database: %w", ctx.Err())
		case <-time.After(interval):
		}
	}
	pool.Close()
	return nil, fmt.Errorf("connect database: %w", last)
}

// Close releases the pool.
func (db *DB) Close() {
	if db != nil && db.pool != nil {
		db.pool.Close()
	}
}

// Ping checks that the pool can reach PostgreSQL.
func (db *DB) Ping(ctx context.Context) error {
	if err := db.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Migrate applies embedded goose migrations.
func Migrate(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// WithinTransaction runs fn in one READ COMMITTED transaction.
// Wallet locks taken inside fn are the concurrency control for debits.
func (db *DB) WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos port.Repositories) error) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", mapError(err, "transaction", ""))
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()

	if _, err := tx.Exec(ctx,
		"SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)",
		db.lockTimeout.String(),
		db.statementTimeout.String(),
	); err != nil {
		return fmt.Errorf("set transaction timeouts: %w", mapError(err, "transaction", ""))
	}

	if err := fn(ctx, newTxRepos(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", mapError(err, "transaction", ""))
	}
	return nil
}

// WalletReader returns wallet reads that use the pool.
func (db *DB) WalletReader() port.WalletReader {
	return walletReader{q: db.pool}
}

// TransferReader returns transfer reads that use the pool.
func (db *DB) TransferReader() port.TransferReader {
	return transferReader{q: db.pool}
}

// LedgerReader returns ledger reads that use the pool.
func (db *DB) LedgerReader() port.LedgerReader {
	return ledgerReader{q: db.pool}
}
