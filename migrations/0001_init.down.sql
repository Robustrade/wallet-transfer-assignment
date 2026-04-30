-- Drop the wallet-transfer schema. Reverse order of creation to respect FKs.

DROP INDEX IF EXISTS idx_transfers_from_wallet;
DROP INDEX IF EXISTS idx_transfers_to_wallet;
DROP INDEX IF EXISTS idx_ledger_wallet_created;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
