DROP TRIGGER IF EXISTS ledger_entries_append_only ON ledger_entries;
DROP FUNCTION IF EXISTS reject_ledger_mutation();

DROP TRIGGER IF EXISTS transfers_enforce_invariants ON transfers;
DROP FUNCTION IF EXISTS enforce_transfer_invariants();

DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;

DROP TYPE IF EXISTS wallet_kind;
DROP TYPE IF EXISTS ledger_entry_type;
DROP TYPE IF EXISTS transfer_status;
