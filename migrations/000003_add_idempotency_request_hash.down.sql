-- 000003_add_idempotency_request_hash.down.sql
-- Removes the request_hash column added in migration 000003.

BEGIN;

ALTER TABLE idempotency_records
    DROP COLUMN IF EXISTS request_hash;

COMMIT;
