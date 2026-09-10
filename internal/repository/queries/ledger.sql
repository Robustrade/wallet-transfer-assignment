-- name: CreateLedgerEntry :one
INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, transfer_id, wallet_id, type, amount, created_at;

-- name: GetLedgerEntriesByTransferID :many
SELECT id, transfer_id, wallet_id, type, amount, created_at
FROM ledger_entries
WHERE transfer_id = $1
ORDER BY created_at ASC;
