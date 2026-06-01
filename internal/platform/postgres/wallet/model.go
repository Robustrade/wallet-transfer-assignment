package wallet

import (
	"time"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
)

// GORM model for the wallets table.
type Model struct {
	ID        string    `gorm:"primaryKey;type:varchar(64)"`
	OwnerID   string    `gorm:"type:varchar(64);not null"`
	Currency  string    `gorm:"type:varchar(8);not null"`
	Status    string    `gorm:"type:varchar(16);not null"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (Model) TableName() string {
	return "wallets"
}

// ToDomain converts the GORM model to the domain Wallet entity.
func (m *Model) ToDomain() *wallet.Wallet {
	return &wallet.Wallet{
		ID:        m.ID,
		OwnerID:   m.OwnerID,
		Currency:  m.Currency,
		Status:    wallet.Status(m.Status),
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

// FromDomain converts a domain Wallet into the GORM model.
func FromDomain(w *wallet.Wallet) *Model {
	return &Model{
		ID:        w.ID,
		OwnerID:   w.OwnerID,
		Currency:  w.Currency,
		Status:    string(w.Status),
		CreatedAt: w.CreatedAt,
		UpdatedAt: w.UpdatedAt,
	}
}
