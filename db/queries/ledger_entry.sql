-- name: CreateLedgerRecord :exec
    INSERT INTO ledger_entry(entry_id,wallet_id,transaction_id,amount,entry_type)
    VALUES(?,?,?,?,?);


-- name: GetStatementByWalletId :many
SELECT
    l.created_at,
    l.entry_id,
    l.transaction_id,
    l.amount,
    l.entry_type,
    l.balance_after,
    l.created_at,

    t.from_wallet_id,
    t.to_wallet_id,
    t.status,
    t.comments

FROM ledger_entry AS l
JOIN transfer AS t
    ON l.transaction_id = t.transaction_id
WHERE l.wallet_id = ?
ORDER BY l.created_at DESC;


-- name: GetStatement :many
SELECT
    l.created_at,
      l.transaction_id,
    l.entry_id,
    l.amount,
    l.entry_type,
    l.balance_after,
    l.created_at,
    t.from_wallet_id,
    t.to_wallet_id,
    t.status,
    t.comments

FROM ledger_entry AS l
JOIN transfer AS t
    ON l.transaction_id = t.transaction_id
ORDER BY l.created_at DESC;