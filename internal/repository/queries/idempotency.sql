-- name: GetIdempotencyRecord :one
SELECT key, request_hash, response_code, response_body, created_at
FROM idempotency_records
WHERE key = $1;

-- name: CreateIdempotencyRecord :one
INSERT INTO idempotency_records (key, request_hash, response_code, response_body)
VALUES ($1, $2, $3, $4)
RETURNING key, request_hash, response_code, response_body, created_at;
