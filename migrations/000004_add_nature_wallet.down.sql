-- 000004_add_nature_wallet.down.sql
-- Removes the nature wallet seeded by the up migration.
-- Any transfers that reference this wallet as from_wallet_id will prevent
-- deletion due to the FK constraint on the transfers table; run this only
-- against a clean development database.

BEGIN;

DELETE FROM wallets WHERE id = 'c0ffee00-0000-0000-0000-000000000001';

COMMIT;
