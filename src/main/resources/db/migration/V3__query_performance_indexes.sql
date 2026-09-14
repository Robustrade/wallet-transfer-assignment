-- P0/P1/P2 query indexes aligned with repository access patterns.
-- Entity @Table(indexes/uniqueConstraints) mirror these names for discoverability
-- (schema remains owned by Flyway; Hibernate ddl-auto is none).

-- P0: wallet create (existsByUserId) + statement lookup by userId
CREATE UNIQUE INDEX IF NOT EXISTS wallets_user_id_uk ON wallets (user_id);

-- P1: statement ledger read ordered by created_at, id (replaces wallet_id-only index)
CREATE INDEX IF NOT EXISTS ledger_entries_wallet_created_id_idx
    ON ledger_entries (wallet_id, created_at ASC, id ASC);
DROP INDEX IF EXISTS ledger_entries_wallet_id_idx;

-- P2: FK columns used for wallet-scoped transfer history / join performance
CREATE INDEX IF NOT EXISTS transfers_from_wallet_idx ON transfers (from_wallet);
CREATE INDEX IF NOT EXISTS transfers_to_wallet_idx ON transfers (to_wallet);
