-- V1__init.sql — Wallet Transfer Service schema.
--
-- Tables: clients, wallets, transfers, ledger_entries, idempotency_records.
-- Integrity invariants are enforced by CHECK / UNIQUE / FK constraints and by
-- triggers (immutable ledger, auto-touched updated_at). Statuses, balance>=0,
-- amount>0, from<>to, one ledger row per (transfer, wallet) all live in the
-- DDL — application code should never need to re-validate them.
--
-- Two design choices worth pinning here for future readers:
--
-- 1. UUID primary keys on transfers and ledger_entries are NOT NULL with NO
--    server-side DEFAULT. Both tables have a paired domain class that assigns
--    UUID.randomUUID() at construction (Transfer.processed / Transfer.failed
--    and the single-arg LedgerEntry constructor). A DB-side
--    DEFAULT gen_random_uuid() was considered and rejected: it would mask a
--    class of insertion bugs by silently filling in a UUID for any code path
--    that forgot to set it, producing a row whose id no caller held a
--    reference to (so the row could not be linked back, audited, or retrieved
--    by primary key from the originating request). Without the default, any
--    such omission is a fast, loud NOT NULL violation at insert time.
--
-- 2. The two transfer-history indexes are covering (INCLUDE clause) from
--    inception. TransferRepository.findByWalletId UNION-ALLs two index-driven
--    sub-queries ordered by (wallet, created_at desc); each leg fetches every
--    transfer column read by the SELECT (status, amount, updated_at, …). With
--    a plain B-tree on (wallet_id, created_at) Postgres still has to do a heap
--    fetch per row to materialise the non-key columns; promoting both indexes
--    to covering lets each leg satisfy the typical query with index-only scans,
--    which is the win as the history limit (configurable via
--    wallet.transfer-history.max-limit, default 500) grows.
--
--    The DESC direction on created_at matches the dominant query order
--    (history is read newest-first via ORDER BY created_at DESC LIMIT n). It is
--    NOT load-bearing: Postgres can scan an ascending B-tree backwards at
--    near-identical cost. Future readers should NOT cargo-cult DESC into
--    unrelated indexes — pick whichever direction matches the dominant scan.
--
--    Notably, error_message (TEXT) is intentionally NOT in the INCLUDE list:
--      a. TEXT can be TOAST'd; even if it sits in the index payload, the heap
--         fetch is unavoidable for out-of-line values and would defeat the
--         index-only-scan benefit on the common case.
--      b. Indexes on CLOB-like types are also not supported by jOOQ's
--         H2-based DDL simulator used during codegen.
--    The few rows where error_message is non-null (FAILED transfers) will pay
--    a single heap fetch each — acceptable for a column that is rarely the
--    hot-path read.

CREATE TABLE clients (
    id          VARCHAR(64) PRIMARY KEY,
    name        VARCHAR(128),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id               UUID        PRIMARY KEY,
    from_wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id     VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount           BIGINT      NOT NULL CHECK (amount > 0),
    status           VARCHAR(16) NOT NULL DEFAULT 'PENDING'
                        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_message    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet_created
    ON transfers (from_wallet_id, created_at DESC)
    INCLUDE (id, to_wallet_id, amount, status, updated_at);

CREATE INDEX idx_transfers_to_wallet_created
    ON transfers (to_wallet_id, created_at DESC)
    INCLUDE (id, from_wallet_id, amount, status, updated_at);

CREATE TABLE ledger_entries (
    id              UUID        PRIMARY KEY,
    wallet_id       VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transfer_id     UUID        NOT NULL REFERENCES transfers(id),
    entry_type      VARCHAR(8)  NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    running_balance BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_ledger_per_transfer_wallet UNIQUE (transfer_id, wallet_id)
);

CREATE INDEX idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);

CREATE TABLE idempotency_records (
    client_id        VARCHAR(64)  NOT NULL,
    idempotency_key  VARCHAR(256) NOT NULL,
    request_hash     VARCHAR(64)  NOT NULL,
    transfer_id      UUID         REFERENCES transfers(id),
    status           VARCHAR(32)  NOT NULL DEFAULT 'IN_PROGRESS'
                        CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_status  INTEGER,
    response_body    TEXT,
    times_seen       INTEGER      NOT NULL DEFAULT 1,
    first_seen_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_seen_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),

    CONSTRAINT pk_idempotency_records PRIMARY KEY (client_id, idempotency_key),
    CONSTRAINT fk_idempotency_client  FOREIGN KEY (client_id) REFERENCES clients(id)
);

/* [jooq ignore start] */
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_wallets_updated_at
BEFORE UPDATE ON wallets
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_update_transfers_updated_at
BEFORE UPDATE ON transfers
FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE OR REPLACE FUNCTION block_ledger_modifications()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Ledger entries are immutable. UPDATE and DELETE operations are prohibited.';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_block_ledger_update
BEFORE UPDATE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION block_ledger_modifications();

CREATE TRIGGER trg_block_ledger_delete
BEFORE DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION block_ledger_modifications();
/* [jooq ignore stop] */
