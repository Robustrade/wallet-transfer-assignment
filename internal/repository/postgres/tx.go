package postgres

import (
	"context"

	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type txImpl struct {
	tx *gorm.DB
}

func (t *txImpl) Commit(ctx context.Context) error {
	return t.tx.WithContext(ctx).Commit().Error
}

func (t *txImpl) Rollback(ctx context.Context) error {
	return t.tx.WithContext(ctx).Rollback().Error
}

func (t *txImpl) GetBalance(ctx context.Context, walletID int64) (int64, error) {
	var wallet model.Wallet
	err := t.tx.WithContext(ctx).
		Where("id = ?", walletID).
		First(&wallet).Error

	return wallet.Balance, err
}

func (t *txImpl) GetWalletForUpdate(ctx context.Context, id int64) (int64, error) {
	var wallet model.Wallet
	err := t.tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		First(&wallet).Error

	return wallet.Balance, err
}

func (t *txImpl) UpdateWallet(ctx context.Context, id int64, balance int64) error {
	return t.tx.WithContext(ctx).
		Model(&model.Wallet{}).
		Where("id = ?", id).
		Update("balance", balance).Error
}

func (t *txImpl) CreateTransfer(ctx context.Context, from, to, amount int64, key string) (int64, error) {
	transfer := &model.Transfer{
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         amount,
		State:          "PENDING",
		IdempotencyKey: key,
	}

	result := t.tx.WithContext(ctx).Create(transfer)
	if result.Error != nil {
		return 0, result.Error
	}

	return transfer.ID, nil
}

func (t *txImpl) UpdateTransferState(ctx context.Context, transferID int64, state string) error {
	return t.tx.WithContext(ctx).
		Model(&model.Transfer{}).
		Where("id = ?", transferID).
		Update("state", state).Error
}

func (t *txImpl) InsertLedgerEntry(ctx context.Context, walletID, transferID int64, typ string, amount int64) error {
	entry := &model.LedgerEntry{
		WalletID:   walletID,
		TransferID: transferID,
		Type:       typ,
		Amount:     amount,
	}

	return t.tx.WithContext(ctx).Create(entry).Error
}
