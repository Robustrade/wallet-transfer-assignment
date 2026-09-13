-- name: CreateIdempotencyRecord :exec
INSERT INTO idempotency_record (
    idempotency_key,
    transaction_id
)
VALUES (?, ?);

-- name: GetIdempotencyRecordById :one 
select idempotency_key,
    transaction_id,
    created_at,
    updated_at from idempotency_record where idempotency_key=?