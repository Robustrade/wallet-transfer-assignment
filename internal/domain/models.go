package domain

import "time"

type TransferState string

const (
	PENDING   TransferState = "PENDING"
	PROCESSED TransferState = "PROCESSED"
	FAILED    TransferState = "FAILED"
)

// ---------------- WALLET ----------------

type Wallet struct {
	ID      string `gorm:"primaryKey"`
	Balance int64  `gorm:"not null;check:balance >= 0"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ---------------- TRANSFER ----------------

type Transfer struct {
	ID           string        `gorm:"primaryKey"`
	FromWalletID string        `gorm:"not null;index"`
	ToWalletID   string        `gorm:"not null;index"`
	Amount       int64         `gorm:"not null;check:amount > 0"`
	State        TransferState `gorm:"type:varchar(20);not null"`

	// Associations (logical FK)
	FromWallet Wallet `gorm:"foreignKey:FromWalletID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	ToWallet   Wallet `gorm:"foreignKey:ToWalletID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`

	CreatedAt time.Time
}

// ---------------- LEDGER ----------------

type LedgerType string

const (
	DEBIT  LedgerType = "DEBIT"
	CREDIT LedgerType = "CREDIT"
)

type LedgerEntry struct {
	ID         uint       `gorm:"primaryKey"`
	WalletID   string     `gorm:"not null;index"`
	TransferID string     `gorm:"not null;index"`
	Type       LedgerType `gorm:"type:varchar(10);not null"`
	Amount     int64      `gorm:"not null;check:amount > 0"`

	// Associations
	Wallet   Wallet   `gorm:"foreignKey:WalletID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT"`
	Transfer Transfer `gorm:"foreignKey:TransferID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`

	CreatedAt time.Time
}

// ---------------- IDEMPOTENCY ----------------

type IdempotencyRecord struct {
	Key      string `gorm:"primaryKey"`
	Response string `gorm:"type:text;not null"`

	CreatedAt time.Time
}
