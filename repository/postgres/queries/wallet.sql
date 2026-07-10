-- name: GetWalletByID :one
SELECT id, owner_name, balance, created_at, updated_at
FROM wallets
WHERE id = $1;

-- name: LockWalletByID :one
SELECT id FROM wallets WHERE id = $1 FOR UPDATE;

-- name: GetWalletBalance :one
SELECT balance FROM wallets WHERE id = $1;

-- name: DebitWallet :exec
UPDATE wallets SET balance = balance - $1, updated_at = now() WHERE id = $2;

-- name: CreditWallet :exec
UPDATE wallets SET balance = balance + $1, updated_at = now() WHERE id = $2;
