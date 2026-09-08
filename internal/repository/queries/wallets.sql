-- name: GetWallet :one
SELECT id, balance, created_at, updated_at
FROM wallets
WHERE id = $1;
