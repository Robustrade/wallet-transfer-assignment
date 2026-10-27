-- Remove dependent tables before the transfers and wallets they reference.
-- Dropping the tables also removes their indexes and the ledger identity
-- sequence. Omit CASCADE so future dependent objects must be rolled back first.
DROP TABLE idempotency_records;
DROP TABLE ledger_entries;
DROP TABLE transfers;
DROP TABLE wallets;
