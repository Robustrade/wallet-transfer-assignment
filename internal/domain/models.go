
package domain

type Wallet struct {
    ID      string `db:"id" json:"id"`
    Balance int64  `db:"balance" json:"balance"`
}

type Transfer struct {
    ID             string `db:"id" json:"id"`
    IdempotencyKey string `db:"idempotency_key" json:"idempotencyKey"`
    FromWalletID   string `db:"from_wallet_id" json:"fromWalletId"`
    ToWalletID     string `db:"to_wallet_id" json:"toWalletId"`
    Amount         int64  `db:"amount" json:"amount"`
    Status         string `db:"status" json:"status"`
}
