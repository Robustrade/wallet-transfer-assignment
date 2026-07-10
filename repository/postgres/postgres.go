package postgres

import (
	"database/sql"
	"time"

	_ "github.com/lib/pq"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
)

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

func NewDB(dsn string, pool PoolConfig) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	if err := db.Ping(); err != nil {
		logger.Warn().Err(err).Msg("failed to ping db")
		if closeErr := db.Close(); closeErr != nil {
			logger.Error().Err(closeErr).Msg("error closing db")
		}
		return nil, err
	}
	logger.Info().Int("max_open_conns", pool.MaxOpenConns).Int("max_idle_conns", pool.MaxIdleConns).Msg("connected to db")
	return db, nil
}
