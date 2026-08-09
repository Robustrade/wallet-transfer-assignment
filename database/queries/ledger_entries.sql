-- name: CreateLedgerEntry :exec
INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
VALUES ($1, $2, $3, $4, $5);

-- name: GetLedgerEntriesByTransferID :many
SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
FROM ledger_entries
WHERE transfer_id = $1
ORDER BY created_at, id;

-- name: GetLedgerEntriesByWalletID :many
SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
FROM ledger_entries
WHERE wallet_id = $1
ORDER BY created_at, id;

-- name: CountLedgerEntriesByTransfer :one
SELECT COUNT(*)
FROM ledger_entries
WHERE transfer_id = $1;