-- name: CreateTransfer :one
INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at;

-- name: GetTransferByID :one
SELECT id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at
FROM transfers
WHERE id = $1;

-- name: GetTransferByIdempotencyKey :one
SELECT id, from_wallet_id, to_wallet_id, amount, status, idempotency_key, created_at, updated_at
FROM transfers
WHERE idempotency_key = $1;

-- name: UpdateTransferStatus :one
UPDATE transfers
SET status = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;
