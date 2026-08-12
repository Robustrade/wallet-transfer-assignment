CREATE TABLE IF NOT EXISTS idempotency_records (
    key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    transfer_id UUID NOT NULL UNIQUE REFERENCES transfers(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
