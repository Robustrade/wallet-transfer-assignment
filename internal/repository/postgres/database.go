package postgres

import (
	"context"
	"fmt"
	"sync"
	"time"
	"wallet-transfer-assignment/internal/appErrors"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const SchemaVersion = 2

var (
	initMu   sync.Mutex
	instance *gorm.DB
)

// Init serializes initialization and caches only a successful database handle.
// A failed call returns its error; a later explicit call may try again. There is
// no background retry, and shutdown closes the singleton for this process lifetime.
func Init(ctx context.Context, dsn string, timeout time.Duration) (*gorm.DB, error) {
	initMu.Lock()
	defer initMu.Unlock()

	if instance != nil {
		return instance, nil
	}

	db, err := open(ctx, dsn, timeout)
	if err != nil {
		return nil, err
	}

	instance = db
	return instance, nil
}

// open initializes GORM and verifies connectivity/schema within a bounded deadline.
// Connection-pool settings are left at the ORM/driver defaults.
func open(ctx context.Context, dsn string, timeout time.Duration) (*gorm.DB, error) {
	db, err := gorm.Open(
		postgres.Open(dsn),
		&gorm.Config{
			DisableAutomaticPing: true,                                  // Use the bounded PingContext below instead.
			Logger:               logger.Default.LogMode(logger.Silent), // Driver errors may contain credentials.
		})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", appErrors.ErrDatabaseOpen, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", appErrors.ErrDatabaseOpen, err)
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err = sqlDB.PingContext(checkCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("%w: %w", appErrors.ErrDatabaseConnection, err)
	}
	if err = VerifySchema(checkCtx, db); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func VerifySchema(ctx context.Context, db *gorm.DB) error {
	var version int
	var dirty bool
	if err := db.WithContext(ctx).Raw("SELECT version, dirty FROM schema_migrations WHERE singleton = true").Row().Scan(&version, &dirty); err != nil {
		return fmt.Errorf("%w: %w", appErrors.ErrSchemaUnavailable, err)
	}
	if version != SchemaVersion || dirty {
		return appErrors.ErrSchemaIncompatible
	}
	return nil
}
