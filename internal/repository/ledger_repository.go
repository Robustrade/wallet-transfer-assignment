
package repository

import "github.com/jmoiron/sqlx"

type LedgerRepository struct {
    db *sqlx.DB
}

func NewLedgerRepository(db *sqlx.DB) *LedgerRepository {
    return &LedgerRepository{db: db}
}

func (r *LedgerRepository) CreateEntry(
    tx *sqlx.Tx,
    walletID string,
    transferID string,
    entryType string,
    amount int64,
) error {

    _, err := tx.Exec(
        `INSERT INTO ledger_entries
        (wallet_id, transfer_id, type, amount)
        VALUES ($1, $2, $3, $4)`,
        walletID,
        transferID,
        entryType,
        amount,
    )

    return err
}
