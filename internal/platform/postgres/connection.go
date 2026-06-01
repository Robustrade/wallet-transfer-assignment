package postgres

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/transfer"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/wallet"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/timeutil"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/tx"
	"gorm.io/gorm/clause"
)

// DB wraps a GORM DB instance and implements the tx.Starter interface.
type DB struct {
	gormDB *gorm.DB
}

// NewConnection creates a GORM DB connected to the PostgreSQL DSN.
// It also runs AutoMigrate for all model structs.
func NewConnection(dsn string) (*DB, error) {
	// Configure GORM logger for production-friendly output.
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{SlowThreshold: time.Second, LogLevel: logger.Silent, Colorful: false},
	)
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:         newLogger,
		NamingStrategy: schema.NamingStrategy{SingularTable: false},
	})
	if err != nil {
		return nil, fmt.Errorf("postgres connection failed: %w", err)
	}
	// AutoMigrate all domain models.
	if err := gdb.AutoMigrate(
		&wallet.Model{},
		&transfer.Model{},
		&ledger.EntryModel{},
		&ledger.BalanceModel{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate failed: %w", err)
	}

	// Seed default wallet for testing/faucet purposes
	defaultWallet := wallet.Model{
		ID:        "default_wallet",
		OwnerID:   "0",
		Currency:  "USD",
		Status:    "ACTIVE",
		CreatedAt: timeutil.Now(),
		UpdatedAt: timeutil.Now(),
	}
	if err := gdb.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaultWallet).Error; err != nil {
		return nil, fmt.Errorf("failed to seed default wallet: %w", err)
	}

	defaultBalance := ledger.BalanceModel{
		WalletID:  "default_wallet",
		Amount:    9999999999999999,
		UpdatedAt: timeutil.Now(),
	}
	if err := gdb.Clauses(clause.OnConflict{DoNothing: true}).Create(&defaultBalance).Error; err != nil {
		return nil, fmt.Errorf("failed to seed default wallet balance: %w", err)
	}

	// Configure the underlying connection pool to prevent "too many clients" errors
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}
	
	// PostgreSQL defaults to max 100 connections. Limit GORM to 50 concurrent open connections.
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return &DB{gormDB: gdb}, nil
}

// DB returns the underlying *gorm.DB for direct use when needed.
func (d *DB) DB() *gorm.DB { return d.gormDB }

// BeginTx implements the tx.Starter interface, returning a context containing the transaction and a transaction wrapper.
func (d *DB) BeginTx(ctx context.Context) (context.Context, tx.Tx, error) {
	txGorm := d.gormDB.WithContext(ctx).Begin()
	if txGorm.Error != nil {
		return ctx, nil, txGorm.Error
	}
	txCtx := tx.InjectTx(ctx, txGorm)
	return txCtx, &gormTx{tx: txGorm}, nil
}

// gormTx adapts a *gorm.DB transaction to the tx.Tx interface.
type gormTx struct {
	tx *gorm.DB
}

func (t *gormTx) Commit() error {
	return t.tx.Commit().Error
}

func (t *gormTx) Rollback() error {
	return t.tx.Rollback().Error
}
