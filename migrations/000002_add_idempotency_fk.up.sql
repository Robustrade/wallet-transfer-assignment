-- 000002_add_idempotency_fk.up.sql
-- Adds a foreign key from idempotency_records.transfer_id to transfers(id).
-- This enforces referential integrity: an idempotency record can only exist
-- if the corresponding transfer exists in the same database.
-- ON DELETE CASCADE means cleaning up a transfer also removes its cached response.

BEGIN;

ALTER TABLE idempotency_records
    ADD CONSTRAINT fk_idempotency_transfer
    FOREIGN KEY (transfer_id) REFERENCES transfers(id) ON DELETE CASCADE;

COMMIT;
