-- name: CreateTransfer :one
INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_reason)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_reason, created_at, updated_at;

-- name: GetTransfer :one
SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, error_reason, created_at, updated_at
FROM transfers
WHERE id = $1;
