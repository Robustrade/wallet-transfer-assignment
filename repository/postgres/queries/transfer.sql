-- name: GetTransferByIdempotencyKey :one
SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
FROM transfers
WHERE idempotency_key = $1;

-- name: GetTransferByID :one
SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
FROM transfers
WHERE id = $1;

-- name: ListPendingTransfers :many
SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
FROM transfers
WHERE status = 'PENDING' AND created_at >= now() - interval '30 minutes'
ORDER BY created_at ASC;

-- name: InsertPendingTransfer :one
INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
VALUES ($1, $2, $3, $4, 'PENDING')
RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at;

-- name: MarkTransferProcessed :one
UPDATE transfers
SET status = 'PROCESSED', updated_at = now()
WHERE id = $1 AND status = 'PENDING'
RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at;

-- name: MarkTransferFailed :one
UPDATE transfers
SET status = 'FAILED', failure_reason = $2, updated_at = now()
WHERE id = $1 AND status = 'PENDING'
RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at;

-- name: InsertLedgerEntry :exec
INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
VALUES ($1, $2, $3, $4);

-- name: ListTransfersByWallet :many
SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
FROM transfers
WHERE from_wallet_id = $1 OR to_wallet_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
