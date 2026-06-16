-- V3__enforce_wallet_immutability.sql
--
-- Pin the append-only invariant on `wallets` two ways: a COMMENT for readers
-- of `\d+ wallets` / pg_catalog.pg_description, and a BEFORE DELETE trigger
-- so the database itself rejects any DELETE — the convention cannot be
-- silently broken by an admin script, a Flyway repair, or a future
-- repository method.
--
-- Why this matters in concrete terms: WalletService.getTransferHistory short-
-- circuits the existence check when transfers reference the wallet, on the
-- assumption that "transfer rows imply the wallet still exists". A successful
-- DELETE elsewhere would silently turn that optimisation into a stale-data
-- read — the trigger here closes that loophole at the only place that can
-- actually enforce it.
--
-- Mirrors the existing `block_ledger_modifications` pattern on ledger_entries
-- (V1) so a maintainer reading either trigger immediately recognises the
-- shape: a RAISE EXCEPTION function, BEFORE DELETE row trigger.
--
-- (COMMENT body is a single-line literal; jOOQ's H2-based DDL simulator used
-- during codegen does not parse SQL string concatenation with || in COMMENT
-- statements.)

COMMENT ON TABLE wallets IS 'Append-only: rows are created by seed/admin flows and are NEVER deleted by application code. WalletService.getTransferHistory and any other caller that derives wallet existence from transfer existence depends on this invariant.';

/* [jooq ignore start] */
CREATE OR REPLACE FUNCTION block_wallet_deletion()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Wallets are append-only. DELETE is prohibited — see WalletService.getTransferHistory and migration V3.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_block_wallet_delete
BEFORE DELETE ON wallets
FOR EACH ROW EXECUTE FUNCTION block_wallet_deletion();
/* [jooq ignore stop] */
