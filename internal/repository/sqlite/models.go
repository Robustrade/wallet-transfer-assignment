package sqlite

import (
	"time"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
)

type walletModel struct {
	ID        string `gorm:"primaryKey;type:text"`
	Balance   int64  `gorm:"not null;check:balance >= 0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (walletModel) TableName() string {
	return "wallets"
}

type transferModel struct {
	ID             string `gorm:"primaryKey;type:text"`
	IdempotencyKey string `gorm:"not null;uniqueIndex;type:text"`
	FromWalletID   string `gorm:"not null;type:text;check:from_wallet_id <> to_wallet_id"`
	ToWalletID     string `gorm:"not null;type:text"`
	Amount         int64  `gorm:"not null;check:amount > 0"`
	State          string `gorm:"not null;check:state IN ('PENDING','PROCESSED','FAILED')"`
	FailureReason  string `gorm:"not null;default:''"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (transferModel) TableName() string {
	return "transfers"
}

type ledgerEntryModel struct {
	ID         int64  `gorm:"primaryKey;autoIncrement"`
	WalletID   string `gorm:"not null;type:text;index"`
	Wallet     walletModel `gorm:"foreignKey:WalletID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	TransferID string `gorm:"not null;type:text;index;uniqueIndex:idx_ledger_transfer_type"`
	Transfer   transferModel `gorm:"foreignKey:TransferID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	Type       string `gorm:"not null;type:text;check:type IN ('DEBIT','CREDIT');uniqueIndex:idx_ledger_transfer_type"`
	Amount     int64  `gorm:"not null;check:amount > 0"`
	CreatedAt  time.Time
}

func (ledgerEntryModel) TableName() string {
	return "ledger_entries"
}

type idempotencyRecordModel struct {
	Key         string `gorm:"primaryKey;type:text"`
	RequestHash string `gorm:"not null;type:text"`
	TransferID  string `gorm:"not null;type:text;uniqueIndex"`
	CreatedAt   time.Time
}

func (idempotencyRecordModel) TableName() string {
	return "idempotency_records"
}

func walletToDomain(model walletModel) domain.Wallet {
	return domain.Wallet{
		ID:        model.ID,
		Balance:   model.Balance,
		CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
	}
}

func transferToDomain(model transferModel) domain.Transfer {
	return domain.Transfer{
		ID:             model.ID,
		IdempotencyKey: model.IdempotencyKey,
		FromWalletID:   model.FromWalletID,
		ToWalletID:     model.ToWalletID,
		Amount:         model.Amount,
		State:          domain.TransferState(model.State),
		FailureReason:  model.FailureReason,
		CreatedAt:      model.CreatedAt,
		UpdatedAt:      model.UpdatedAt,
	}
}

func ledgerEntryToDomain(model ledgerEntryModel) domain.LedgerEntry {
	return domain.LedgerEntry{
		ID:         model.ID,
		WalletID:   model.WalletID,
		TransferID: model.TransferID,
		Type:       domain.LedgerEntryType(model.Type),
		Amount:     model.Amount,
		CreatedAt:  model.CreatedAt,
	}
}

func idempotencyRecordToDomain(model idempotencyRecordModel) domain.IdempotencyRecord {
	return domain.IdempotencyRecord{
		Key:         model.Key,
		RequestHash: model.RequestHash,
		TransferID:  model.TransferID,
		CreatedAt:   model.CreatedAt,
	}
}
