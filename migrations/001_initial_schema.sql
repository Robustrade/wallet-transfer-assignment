-- 001_initial_schema.sql

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- Wallets
-- ============================================================

CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Amount is stored in the smallest currency unit.
    -- Example: 10000 represents 100.00 for a 2-decimal currency.
    balance BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT wallets_balance_non_negative
        CHECK (balance >= 0)
);


-- ============================================================
-- Transfers
-- ============================================================

CREATE TABLE transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,

    amount BIGINT NOT NULL,

    status TEXT NOT NULL DEFAULT 'PENDING',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT transfers_from_wallet_fk
        FOREIGN KEY (from_wallet_id)
        REFERENCES wallets(id),

    CONSTRAINT transfers_to_wallet_fk
        FOREIGN KEY (to_wallet_id)
        REFERENCES wallets(id),

    CONSTRAINT transfers_amount_positive
        CHECK (amount > 0),

    CONSTRAINT transfers_different_wallets
        CHECK (from_wallet_id <> to_wallet_id),

    CONSTRAINT transfers_valid_status
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);


-- ============================================================
-- Ledger
-- ============================================================

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,

    entry_type TEXT NOT NULL,
    amount BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_transfer_fk
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CONSTRAINT ledger_wallet_fk
        FOREIGN KEY (wallet_id)
        REFERENCES wallets(id),

    CONSTRAINT ledger_valid_entry_type
        CHECK (entry_type IN ('DEBIT', 'CREDIT')),

    CONSTRAINT ledger_amount_positive
        CHECK (amount > 0),

    -- A transfer can have at most one DEBIT and one CREDIT.
    CONSTRAINT ledger_unique_transfer_entry_type
        UNIQUE (transfer_id, entry_type)
);


-- ============================================================
-- Idempotency
-- ============================================================

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,

    -- Hash of the normalized business request:
    -- fromWalletId + toWalletId + amount
    request_hash TEXT NOT NULL,

    transfer_id UUID,

    response_code INTEGER,
    response_body JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Retention metadata. We will NOT automatically delete
    -- records as part of this assignment.
    expires_at TIMESTAMPTZ,

    CONSTRAINT idempotency_transfer_fk
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CONSTRAINT idempotency_response_code_valid
        CHECK (
            response_code IS NULL
            OR response_code BETWEEN 100 AND 599
        )
);


-- ============================================================
-- Indexes
-- ============================================================

CREATE INDEX idx_transfers_from_wallet
    ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet
    ON transfers(to_wallet_id);

CREATE INDEX idx_transfers_status
    ON transfers(status);

CREATE INDEX idx_ledger_wallet
    ON ledger_entries(wallet_id);

CREATE INDEX idx_idempotency_transfer
    ON idempotency_records(transfer_id);

CREATE INDEX idx_idempotency_expires_at
    ON idempotency_records(expires_at)
    WHERE expires_at IS NOT NULL;