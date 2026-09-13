-- name: CreateTransfer :one
INSERT INTO transfer ( transaction_id, from_wallet_id, to_wallet_id, amount,status)
VALUES (?,?,?,?,?)
RETURNING transaction_id, from_wallet_id, to_wallet_id, amount, status;


-- name: GetTransactions :many
select transaction_id,from_wallet_id,to_wallet_id,amount,status,comments 
from transfer;


-- name: GetTransactionById :one
select transaction_id,from_wallet_id,to_wallet_id,amount,status,comments
from transfer where transaction_id=?;

