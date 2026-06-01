package ledger

import "time"

// EntryModel maps to the `ledger_entries` table (double‑entry rows).
type EntryModel struct {
	ID         uint      `gorm:"primaryKey;autoIncrement"`
	TransferID string    `gorm:"type:varchar(64);index;not null"`
	WalletID   string    `gorm:"type:varchar(64);not null"`
	Type       string    `gorm:"type:varchar(8);not null"` // DEBIT or CREDIT
	Amount     int64     `gorm:"not null"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

// BalanceModel maps to the `wallet_balances` table.
type BalanceModel struct {
	WalletID  string    `gorm:"primaryKey;type:varchar(64)"`
	Amount    int64     `gorm:"not null"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

// TableName overrides the default GORM table name.
func (EntryModel) TableName() string {
	return "ledger_entries"
}

// TableName overrides the default GORM table name.
func (BalanceModel) TableName() string {
	return "wallet_balances"
}

// // ToDomain converts a GORM entry into the domain Ledger Entry.
// func (e *EntryModel) ToDomain() *ledger.Entry {
//     return &ledger.Entry{
//         ID:         fmt.Sprintf("%d", e.ID),
//         TransferID: e.TransferID,
//         WalletID:   e.WalletID,
//         Type:       ledger.EntryType(e.Type),
//         Amount:     e.Amount,
//         CreatedAt:  e.CreatedAt,
//     }
// }
