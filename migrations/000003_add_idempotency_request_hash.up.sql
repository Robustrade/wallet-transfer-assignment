-- 000003_add_idempotency_request_hash.up.sql
-- Adds a request_hash column to idempotency_records so that the service can
-- detect when the same idempotency key is reused with different request
-- parameters (different source wallet, destination wallet, or amount).
--
-- The hash is a SHA-256 hex digest of the canonical request fields.
-- DEFAULT '' covers any rows inserted before this migration (none in practice
-- since this runs at first deployment).

BEGIN;

ALTER TABLE idempotency_records
    ADD COLUMN request_hash TEXT NOT NULL DEFAULT '';

COMMIT;
