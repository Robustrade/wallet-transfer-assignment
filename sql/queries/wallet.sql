-- name: GetWalletForUpdate :one
SELECT *
FROM wallets
WHERE id = $1
FOR UPDATE;

-- name: GetWallet :one
SELECT *
FROM wallets
WHERE id = $1;

-- name: UpdateWalletBalance :exec
UPDATE wallets
SET balance = $2,
    updated_at = NOW()
WHERE id = $1;

-- name: CreateWallet :one
INSERT INTO wallets (
    id,
    balance
)
VALUES ($1, $2)
RETURNING *;