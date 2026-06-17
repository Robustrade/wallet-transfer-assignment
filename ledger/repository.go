package ledger

import (
	"gorm.io/gorm"
)

type LedgerRepo struct {
}

func (r *LedgerRepo) CreateEntries(
	tx *gorm.DB,
	entries LedgerEntry,
) error {

	query := `
        INSERT INTO ledger_entries(
            wallet_id,
            transfer_id,
            entry_type,
            amount
        )
        VALUES ($1,$2,$3,$4)
    `

	err := tx.Exec(query,
		entries.WalletID,
		entries.TransferID,
		entries.Type,
		entries.Amount,
	).Error

	if err != nil {
		return err
	}

	return nil
}
