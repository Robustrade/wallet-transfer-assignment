package postgres

import (
	"time"

	"github.com/google/uuid"
)

// These are GORM's row-mapped models. They exist purely to give GORM typed
// access to tables whose actual shape — ENUM types, CHECK constraints,
// triggers — is owned entirely by the hand-written SQL in migrations/ (see
// migrate.go). GORM never AutoMigrates these; the schema is the migration
// files' responsibility, not the model tags'.
type walletModel struct {
	ID           string    `gorm:"column:id;primaryKey"`
	Kind         string    `gorm:"column:kind"`
	Currency     string    `gorm:"column:currency"`
	BalanceMinor int64     `gorm:"column:balance_minor"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (walletModel) TableName() string { return "wallets" }

type transferModel struct {
	ID             uuid.UUID  `gorm:"column:id;primaryKey"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	RequestHash    string     `gorm:"column:request_hash"`
	FromWalletID   string     `gorm:"column:from_wallet_id"`
	ToWalletID     string     `gorm:"column:to_wallet_id"`
	AmountMinor    int64      `gorm:"column:amount_minor"`
	Currency       string     `gorm:"column:currency"`
	Status         string     `gorm:"column:status"`
	FailureCode    *string    `gorm:"column:failure_code"`
	FailureMessage *string    `gorm:"column:failure_message"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	SettledAt      *time.Time `gorm:"column:settled_at"`
}

func (transferModel) TableName() string { return "transfers" }

type ledgerEntryModel struct {
	ID           int64     `gorm:"column:id;primaryKey"`
	TransferID   uuid.UUID `gorm:"column:transfer_id"`
	WalletID     string    `gorm:"column:wallet_id"`
	EntryType    string    `gorm:"column:entry_type"`
	AmountMinor  int64     `gorm:"column:amount_minor"`
	Currency     string    `gorm:"column:currency"`
	BalanceAfter int64     `gorm:"column:balance_after"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (ledgerEntryModel) TableName() string { return "ledger_entries" }
