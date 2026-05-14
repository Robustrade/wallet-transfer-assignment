-- Drop indexes
DROP INDEX IF EXISTS idx_transfers_idempotency_key;
DROP INDEX IF EXISTS idx_ledger_entries_wallet_id;
DROP INDEX IF EXISTS idx_ledger_entries_transfer_id;
DROP INDEX IF EXISTS idx_transfers_from_wallet;
DROP INDEX IF EXISTS idx_transfers_to_wallet;
DROP INDEX IF EXISTS idx_idempotency_records_created_at;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
