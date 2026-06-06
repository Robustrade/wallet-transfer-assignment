-- 000002_add_idempotency_fk.down.sql
-- Removes the FK constraint added in 000002 up migration.

BEGIN;

ALTER TABLE idempotency_records
    DROP CONSTRAINT IF EXISTS fk_idempotency_transfer;

COMMIT;
