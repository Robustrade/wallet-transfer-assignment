package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wallet-transfer/internal/wallet"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/logger"
)

// WalletRepository defines persistence operations for wallets.
type WalletRepository interface {
	Create(ctx context.Context, db database.DBTX, w *wallet.Wallet) error
	GetByID(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error)
	GetByIDForUpdate(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error)
	UpdateBalance(ctx context.Context, db database.DBTX, id string, newBalance float64, updatedBy string) error
	GetAll(ctx context.Context, db database.DBTX) ([]*wallet.Wallet, error)
}

type walletRepository struct{}

// NewWalletRepository returns a new WalletRepository.
func NewWalletRepository() WalletRepository {
	return &walletRepository{}
}

func (r *walletRepository) Create(ctx context.Context, db database.DBTX, w *wallet.Wallet) error {
	const fn = "walletRepository.Create"
	sql := `
		INSERT INTO wallets (id, owner_name, owner_email, balance, created_by, updated_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	now := time.Now().UTC()
	_, err := db.Exec(ctx, sql, w.ID, w.OwnerName, w.OwnerEmail, w.Balance,
		w.AuditLog.CreatedBy, w.AuditLog.UpdatedBy, now, now)
	if err != nil {
		logger.Error(fn, "insert wallet failed", err, map[string]interface{}{"wallet_id": w.ID})
		return err
	}
	w.AuditLog.CreatedAt = now
	w.AuditLog.UpdatedAt = now
	logger.Info(fn, "wallet created", map[string]interface{}{"wallet_id": w.ID})
	return nil
}

func (r *walletRepository) GetByID(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error) {
	const fn = "walletRepository.GetByID"
	sql := `SELECT id, owner_name, owner_email, balance, created_by, updated_by, created_at, updated_at
	        FROM wallets WHERE id = $1`
	row := db.QueryRow(ctx, sql, id)
	w, err := scanWallet(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		logger.Error(fn, "query wallet failed", err, map[string]interface{}{"wallet_id": id})
		return nil, err
	}
	return w, nil
}

// GetByIDForUpdate fetches a wallet with a row-level lock (SELECT … FOR UPDATE).
// Must be called within a transaction.
func (r *walletRepository) GetByIDForUpdate(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error) {
	const fn = "walletRepository.GetByIDForUpdate"
	sql := `SELECT id, owner_name, owner_email, balance, created_by, updated_by, created_at, updated_at
	        FROM wallets WHERE id = $1 FOR UPDATE`
	row := db.QueryRow(ctx, sql, id)
	w, err := scanWallet(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		logger.Error(fn, "lock wallet failed", err, map[string]interface{}{"wallet_id": id})
		return nil, err
	}
	return w, nil
}

func (r *walletRepository) UpdateBalance(ctx context.Context, db database.DBTX, id string, newBalance float64, updatedBy string) error {
	const fn = "walletRepository.UpdateBalance"
	sql := `UPDATE wallets SET balance = $1, updated_by = $2, updated_at = $3 WHERE id = $4`
	_, err := db.Exec(ctx, sql, newBalance, updatedBy, time.Now().UTC(), id)
	if err != nil {
		logger.Error(fn, "update balance failed", err, map[string]interface{}{"wallet_id": id})
		return err
	}
	return nil
}

func (r *walletRepository) GetAll(ctx context.Context, db database.DBTX) ([]*wallet.Wallet, error) {
	const fn = "walletRepository.GetAll"
	sql := `SELECT id, owner_name, owner_email, balance, created_by, updated_by, created_at, updated_at
	        FROM wallets ORDER BY created_at DESC`
	rows, err := db.Query(ctx, sql)
	if err != nil {
		logger.Error(fn, "query all wallets failed", err, nil)
		return nil, err
	}
	defer rows.Close()

	var wallets []*wallet.Wallet
	for rows.Next() {
		w, err := scanWallet(rows)
		if err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	return wallets, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanWallet(row rowScanner) (*wallet.Wallet, error) {
	var w wallet.Wallet
	var ownerName, ownerEmail *string
	err := row.Scan(&w.ID, &ownerName, &ownerEmail, &w.Balance,
		&w.AuditLog.CreatedBy, &w.AuditLog.UpdatedBy, &w.AuditLog.CreatedAt, &w.AuditLog.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if ownerName != nil {
		w.OwnerName = *ownerName
	}
	if ownerEmail != nil {
		w.OwnerEmail = *ownerEmail
	}
	return &w, nil
}
