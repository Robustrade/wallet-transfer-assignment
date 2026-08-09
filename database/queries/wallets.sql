-- name: GetWallet :one
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id = $1
LIMIT 1;

-- name: CreateWallet :one
INSERT INTO wallets (id, balance)
VALUES ($1, $2)
RETURNING id, balance, created_at, updated_at;

-- name: GetWalletsForUpdate :many
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id = ANY($1::text[])
FOR UPDATE;

-- name: SetWalletBalance :exec
UPDATE wallets
SET balance = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;
