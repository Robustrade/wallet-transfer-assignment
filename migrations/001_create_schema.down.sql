-- Drop tables in reverse order (respecting foreign key dependencies)

DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
