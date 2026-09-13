-- name: GetWallet :one
SELECT
    wallet_id,
    balance,
    created_at,
    updated_at
FROM wallet
WHERE wallet_id = ?;


-- name: GetWallets :many
SELECT
    wallet_id,
    balance,
    created_at,
    updated_at
FROM wallet;


-- name: UpdateWallet :exec
UPDATE wallet
SET balance = ?
WHERE wallet_id = ?;

-- name: DebitWallet :execrows
UPDATE wallet
SET balance = balance - ?
WHERE wallet_id = ?
  AND balance >= ?
  RETURNING balance;


-- name: CreditWallet :execrows
UPDATE wallet
SET balance = balance + ?
WHERE wallet_id = ?
RETURNING balance;