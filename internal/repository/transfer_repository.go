
package repository

import (
    "wallet-transfer-assignment/internal/domain"

    "github.com/jmoiron/sqlx"
)

type TransferRepository struct {
    db *sqlx.DB
}

func NewTransferRepository(db *sqlx.DB) *TransferRepository {
    return &TransferRepository{db: db}
}

func (r *TransferRepository) Create(
    tx *sqlx.Tx,
    transfer *domain.Transfer,
) error {

    _, err := tx.Exec(
        `INSERT INTO transfers
        (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
        VALUES ($1, $2, $3, $4, $5, $6)`,
        transfer.ID,
        transfer.IdempotencyKey,
        transfer.FromWalletID,
        transfer.ToWalletID,
        transfer.Amount,
        transfer.Status,
    )

    return err
}

func (r *TransferRepository) GetByIdempotencyKey(
    key string,
) (*domain.Transfer, error) {

    var transfer domain.Transfer

    err := r.db.Get(
        &transfer,
        "SELECT * FROM transfers WHERE idempotency_key = $1",
        key,
    )

    return &transfer, err
}
