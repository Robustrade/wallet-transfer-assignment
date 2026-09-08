CREATE TABLE idempotency_records (
    id BIGSERIAL PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    request_hash CHAR(64) NOT NULL,
    transfer_id UUID REFERENCES transfers(id),
    response_status INTEGER NOT NULL DEFAULT 201,
    response_body JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idempotency_records_transfer_idx ON idempotency_records(transfer_id);
