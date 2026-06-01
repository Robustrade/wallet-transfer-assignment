package transfer

import (
	"time"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
)

// Model maps to the `transfers` table.
type Model struct {
	ID            string    `gorm:"primaryKey;type:varchar(64)"`
	TxnID         string    `gorm:"uniqueIndex;type:varchar(64);not null"`
	FromWalletID  string    `gorm:"type:varchar(64);not null"`
	ToWalletID    string    `gorm:"type:varchar(64);not null"`
	Amount        int64     `gorm:"not null"`
	Status        string    `gorm:"type:varchar(16);not null"`
	FailureReason string    `gorm:"type:text"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (Model) TableName() string {
	return "transfers"
}

// ToDomain converts a GORM Transfer model into the domain Transfer entity.
func (m *Model) ToDomain() *transfer.Transfer {
	return &transfer.Transfer{
		ID:            m.ID,
		TxnID:         m.TxnID,
		FromWalletID:  m.FromWalletID,
		ToWalletID:    m.ToWalletID,
		Amount:        m.Amount,
		Status:        transfer.Status(m.Status),
		FailureReason: m.FailureReason,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}

// FromDomain creates a GORM model from a domain Transfer.
func FromDomain(t *transfer.Transfer) *Model {
	return &Model{
		ID:            t.ID,
		TxnID:         t.TxnID,
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		Status:        string(t.Status),
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}
