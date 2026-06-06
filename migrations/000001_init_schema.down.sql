-- 000001_init_schema.down.sql
-- Rolls back the initial schema creation.
-- Tables are dropped in reverse foreign-key dependency order.

BEGIN;

DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;

COMMIT;
