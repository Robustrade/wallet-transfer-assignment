-- name: CreateLedgerEntry :one
INSERT INTO ledger_entries (
    id,
    transfer_id,
    wallet_id,
    entry_type,
    amount
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetLedgerEntriesByTransfer :many
SELECT *
FROM ledger_entries
WHERE transfer_id = $1;