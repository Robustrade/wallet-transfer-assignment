-- Reverse dependency order: children before parents.
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfer;
DROP TABLE IF EXISTS wallet;
DROP TABLE IF EXISTS transaction_type;
DROP TABLE IF EXISTS transfer_state;
DROP FUNCTION IF EXISTS set_updated_at();
