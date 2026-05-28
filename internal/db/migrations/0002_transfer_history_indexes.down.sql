BEGIN;

DROP INDEX IF EXISTS transfers_to_wallet_created_idx;
DROP INDEX IF EXISTS transfers_from_wallet_created_idx;

COMMIT;
