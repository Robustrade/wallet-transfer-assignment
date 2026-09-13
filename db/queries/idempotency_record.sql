-- name: CreateIdempotencyRecord :exec
INSERT INTO idempotency_record (
    idempotency_key,
    transaction_id
)
VALUES (?, ?);