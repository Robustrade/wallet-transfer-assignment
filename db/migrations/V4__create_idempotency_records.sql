-- Idempotency records. transfer_id's FK is DEFERRABLE INITIALLY DEFERRED because the write
-- order (ADR-0002) inserts this row *before* the transfer row exists, referencing a
-- pre-generated UUIDv7 transfer id (ADR-0004). A plain FK would reject the insert immediately;
-- deferring the check to commit lets the transfer row (inserted next, same transaction) land
-- before the constraint is actually checked. This is the mechanism this table exists to
-- support, not an incidental detail — see ADR-0002.
CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_fingerprint TEXT NOT NULL,
    transfer_id UUID NOT NULL UNIQUE REFERENCES transfers (id) DEFERRABLE INITIALLY DEFERRED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
