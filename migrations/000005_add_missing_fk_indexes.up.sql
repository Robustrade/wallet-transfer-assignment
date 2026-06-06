-- 000005_add_missing_fk_indexes.up.sql
-- Adds indexes on foreign-key columns that were missing in the initial schema.
--
-- PostgreSQL does not automatically create indexes on FK columns; without these,
-- the planner must perform a sequential scan:
--   • on transfers when checking referential integrity during a wallet DELETE
--   • on idempotency_records when the ON DELETE CASCADE on fk_idempotency_transfer
--     fires during a transfer DELETE
--
-- These indexes also support the obvious future queries:
--   • "list transfers sent from / received by a wallet"
--   • "find the idempotency record for a given transfer"

BEGIN;

CREATE INDEX idx_transfers_from_wallet    ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet      ON transfers (to_wallet_id);
CREATE INDEX idx_idempotency_transfer_id  ON idempotency_records (transfer_id);

COMMIT;
