BEGIN;

DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;

DELETE FROM schema_migrations WHERE version = 1;

COMMIT;
