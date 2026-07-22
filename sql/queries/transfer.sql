-- name: GetTransferByIdempotencyKey :one
SELECT *
FROM transfers
WHERE idempotency_key = $1;

-- name: CreateTransfer :one
INSERT INTO transfers (
    id,
    idempotency_key,
    from_wallet_id,
    to_wallet_id,
    amount,
    status
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateTransferStatus :exec
UPDATE transfers
SET status = $2,
    updated_at = NOW()
WHERE id = $1;