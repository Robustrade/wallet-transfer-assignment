-- name: CreateWallet :one
INSERT INTO wallets (id, balance)
VALUES ($1, $2)
RETURNING id, balance, created_at, updated_at;

-- name: GetWallet :one
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id = $1;
