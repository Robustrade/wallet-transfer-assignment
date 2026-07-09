package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/port"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	db *gorm.DB
}

type Tx struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	return &Store{db: db}
}

func DSN(path string) string {
	if strings.Contains(path, "?") {
		return path + "&_busy_timeout=5000&_foreign_keys=on"
	}
	return path + "?_busy_timeout=5000&_foreign_keys=on"
}

func (s *Store) Migrate(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Exec(`PRAGMA foreign_keys = ON`).Error; err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := s.db.WithContext(ctx).AutoMigrate(
		&walletModel{},
		&transferModel{},
		&ledgerEntryModel{},
		&idempotencyRecordModel{},
	); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	return nil
}

func (s *Store) SeedWallets(ctx context.Context, wallets map[string]int64) error {
	for id, balance := range wallets {
		err := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&walletModel{ID: id, Balance: balance}).
			Error
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	var wallet walletModel
	if err := s.db.WithContext(ctx).First(&wallet, "id = ?", walletID).Error; err != nil {
		return domain.Wallet{}, mapNotFound(err, domain.ErrWalletNotFound)
	}
	return walletToDomain(wallet), nil
}

func (s *Store) GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error) {
	var transfer transferModel
	if err := s.db.WithContext(ctx).First(&transfer, "id = ?", transferID).Error; err != nil {
		return domain.Transfer{}, mapNotFound(err, domain.ErrTransferNotFound)
	}
	return transferToDomain(transfer), nil
}

func (s *Store) GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return getLedgerEntries(ctx, s.db, transferID)
}

func (s *Store) WithinTransfer(ctx context.Context, fn func(ctx context.Context, tx port.TransferTx) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, &Tx{db: tx})
	}, &sql.TxOptions{Isolation: sql.LevelSerializable})
}

func (t *Tx) GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, bool, error) {
	var record idempotencyRecordModel
	if err := t.db.WithContext(ctx).First(&record, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.IdempotencyRecord{}, false, nil
		}
		return domain.IdempotencyRecord{}, false, err
	}
	return idempotencyRecordToDomain(record), true, nil
}

func (t *Tx) InsertIdempotencyRecord(ctx context.Context, record domain.IdempotencyRecord) error {
	return t.db.WithContext(ctx).Create(&idempotencyRecordModel{
		Key:         record.Key,
		RequestHash: record.RequestHash,
		TransferID:  record.TransferID,
	}).Error
}

func (t *Tx) InsertTransfer(ctx context.Context, transfer domain.Transfer) error {
	return t.db.WithContext(ctx).Create(&transferModel{
		ID:             transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		State:          string(transfer.State),
	}).Error
}

func (t *Tx) GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error) {
	var transfer transferModel
	if err := t.db.WithContext(ctx).First(&transfer, "id = ?", transferID).Error; err != nil {
		return domain.Transfer{}, mapNotFound(err, domain.ErrTransferNotFound)
	}
	return transferToDomain(transfer), nil
}

func (t *Tx) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	var wallet walletModel
	if err := t.db.WithContext(ctx).First(&wallet, "id = ?", walletID).Error; err != nil {
		return domain.Wallet{}, mapNotFound(err, domain.ErrWalletNotFound)
	}
	return walletToDomain(wallet), nil
}

func (t *Tx) DebitWallet(ctx context.Context, walletID string, amount int64) (bool, error) {
	result := t.db.WithContext(ctx).
		Model(&walletModel{}).
		Where("id = ? AND balance >= ?", walletID, amount).
		Updates(map[string]any{
			"balance":    gorm.Expr("balance - ?", amount),
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return result.RowsAffected == 1, result.Error
}

func (t *Tx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	result := t.db.WithContext(ctx).
		Model(&walletModel{}).
		Where("id = ?", walletID).
		Updates(map[string]any{
			"balance":    gorm.Expr("balance + ?", amount),
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (t *Tx) InsertLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error {
	return t.db.WithContext(ctx).Create(&ledgerEntryModel{
		WalletID:   entry.WalletID,
		TransferID: entry.TransferID,
		Type:       string(entry.Type),
		Amount:     entry.Amount,
	}).Error
}

func (t *Tx) MarkTransferProcessed(ctx context.Context, transferID string) error {
	result := t.db.WithContext(ctx).
		Model(&transferModel{}).
		Where("id = ? AND state = ?", transferID, domain.TransferPending).
		Updates(map[string]any{
			"state":      string(domain.TransferProcessed),
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return requireOneRow(result, domain.ErrInvalidTransferTransition)
}

func (t *Tx) MarkTransferFailed(ctx context.Context, transferID string, reason string) error {
	result := t.db.WithContext(ctx).
		Model(&transferModel{}).
		Where("id = ? AND state = ?", transferID, domain.TransferPending).
		Updates(map[string]any{
			"state":          string(domain.TransferFailed),
			"failure_reason": reason,
			"updated_at":     gorm.Expr("CURRENT_TIMESTAMP"),
		})
	return requireOneRow(result, domain.ErrInvalidTransferTransition)
}

func (t *Tx) GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return getLedgerEntries(ctx, t.db, transferID)
}

func getLedgerEntries(ctx context.Context, db *gorm.DB, transferID string) ([]domain.LedgerEntry, error) {
	var models []ledgerEntryModel
	if err := db.WithContext(ctx).Where("transfer_id = ?", transferID).Order("id").Find(&models).Error; err != nil {
		return nil, err
	}

	entries := make([]domain.LedgerEntry, 0, len(models))
	for _, model := range models {
		entries = append(entries, ledgerEntryToDomain(model))
	}
	return entries, nil
}

func mapNotFound(err error, notFound error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return notFound
	}
	return err
}

func requireOneRow(result *gorm.DB, errWhenMissing error) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errWhenMissing
	}
	return nil
}
