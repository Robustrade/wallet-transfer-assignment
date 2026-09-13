-- name: GetWallet :one
SELECT
    wallet_id,
    balance
FROM wallet
WHERE wallet_id = ?;


-- name: GetWallets :many
SELECT
    wallet_id,
    balance
FROM wallet;


-- name: UpdateWallet :exec
UPDATE wallet
SET balance = ?
WHERE wallet_id = ?;