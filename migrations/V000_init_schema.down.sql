-- V000_init_schema.down.sql
-- Rolls back the initial schema creation.
-- Tables are dropped in reverse foreign-key dependency order.

BEGIN;

DROP TABLE IF EXISTS idempotency_records CASCADE;
DROP TABLE IF EXISTS ledger_entries CASCADE;
DROP TABLE IF EXISTS transfers CASCADE;
DROP TABLE IF EXISTS wallets CASCADE;

COMMIT;