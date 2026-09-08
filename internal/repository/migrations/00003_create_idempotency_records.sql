-- +goose Up
CREATE TABLE IF NOT EXISTS idempotency_records (
    key             VARCHAR(255) PRIMARY KEY,
    request_hash    VARCHAR(64) NOT NULL,
    response_code   INT NOT NULL,
    response_body   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS idempotency_records;
