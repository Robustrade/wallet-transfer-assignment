
package repository

import (
    "wallet-transfer-assignment/internal/domain"

    "github.com/jmoiron/sqlx"
)

type WalletRepository struct {
    db *sqlx.DB
}

func NewWalletRepository(db *sqlx.DB) *WalletRepository {
    return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(wallet *domain.Wallet) error {

    _, err := r.db.Exec(
        "INSERT INTO wallets(id, balance) VALUES($1, $2)",
        wallet.ID,
        wallet.Balance,
    )

    return err
}

func (r *WalletRepository) Get(id string) (*domain.Wallet, error) {

    var wallet domain.Wallet

    err := r.db.Get(
        &wallet,
        "SELECT * FROM wallets WHERE id = $1",
        id,
    )

    return &wallet, err
}
