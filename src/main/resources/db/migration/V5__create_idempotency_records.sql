-- V5: Create idempotency_records table
-- Stores the full serialized response for any idempotent operation.
-- Generic design: (idempotency_key + endpoint) is the composite key so this
-- table can serve multiple API operations in the future, not just transfers.
-- expires_at enables TTL-based expiry — expired records are ignored on lookup
-- and can be purged by a scheduled cleanup job.

CREATE TABLE idempotency_records (
    idempotency_key  TEXT        NOT NULL,
    endpoint         TEXT        NOT NULL,
    response_body    TEXT        NOT NULL,   -- full JSON payload of the original response
    http_status      INT         NOT NULL,   -- HTTP status code to replay
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL,

    CONSTRAINT pk_idempotency_records PRIMARY KEY (idempotency_key, endpoint)
);

-- Supports efficient TTL cleanup: DELETE FROM idempotency_records WHERE expires_at < now()
CREATE INDEX idx_idempotency_expires ON idempotency_records (expires_at);

