DO $$
BEGIN
    CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;
-- statement-break

DO $$
BEGIN
    CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;
-- statement-break

CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0)
);
-- statement-break

CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL,
    to_wallet_id TEXT NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status transfer_status NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL
);
-- statement-break

CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (transfer_id, entry_type)
);
-- statement-break

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_fingerprint TEXT NOT NULL,
    transfer_id UUID NOT NULL UNIQUE REFERENCES transfers(id),
    created_at TIMESTAMPTZ NOT NULL
);
-- statement-break

CREATE INDEX IF NOT EXISTS ledger_entries_transfer_id_idx ON ledger_entries(transfer_id);
-- statement-break

CREATE INDEX IF NOT EXISTS transfers_from_wallet_created_at_idx ON transfers(from_wallet_id, created_at DESC);
-- statement-break

CREATE INDEX IF NOT EXISTS transfers_to_wallet_created_at_idx ON transfers(to_wallet_id, created_at DESC);
-- statement-break
