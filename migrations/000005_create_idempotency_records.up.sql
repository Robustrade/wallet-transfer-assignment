CREATE TYPE idempotency_status AS ENUM (
    'IN_PROGRESS',
    'COMPLETED'
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,

    request_fingerprint TEXT NOT NULL,

    transfer_id UUID REFERENCES transfers(id),

    status idempotency_status NOT NULL DEFAULT 'IN_PROGRESS',

    response_snapshot JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);