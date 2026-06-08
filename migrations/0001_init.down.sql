DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;

DROP TYPE IF EXISTS entry_type;
DROP TYPE IF EXISTS transfer_status;

DROP EXTENSION IF EXISTS pgcrypto;
