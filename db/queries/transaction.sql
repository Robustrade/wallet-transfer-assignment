-- name: CreateTransfer :one
INSERT INTO transfer ( transaction_id, from_wallet_id, to_wallet_id, amount,status)
VALUES (?,?,?,?,?)
RETURNING transaction_id, from_wallet_id, to_wallet_id, amount, status;


-- name: GetTransactions :many
select transaction_id,from_wallet_id,to_wallet_id,amount,status,comments ,created_at,
    updated_at
from transfer;


-- name: GetTransactionById :one
select transaction_id,from_wallet_id,to_wallet_id,amount,status,comments,created_at,
    updated_at
from transfer where transaction_id=?;


-- name: UpdateTransferStatus :execrows
UPDATE transfer
SET status = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE transaction_id = ?
  AND status = 'PENDING';


-- name: MarkTransferFailed :execrows
UPDATE transfer
SET status = 'FAILED',
    updated_at = CURRENT_TIMESTAMP
WHERE transaction_id = ?
  AND status = 'PENDING';