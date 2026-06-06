-- 000005_add_missing_fk_indexes.down.sql

BEGIN;

DROP INDEX IF EXISTS idx_idempotency_transfer_id;
DROP INDEX IF EXISTS idx_transfers_to_wallet;
DROP INDEX IF EXISTS idx_transfers_from_wallet;

COMMIT;
