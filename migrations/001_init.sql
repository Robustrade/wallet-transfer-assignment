CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- =========================================================
-- WALLETS
-- =========================================================

CREATE TABLE wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    balance BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT wallets_balance_non_negative
        CHECK (balance >= 0)
);


-- =========================================================
-- TRANSFERS
-- =========================================================

CREATE TABLE transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,

    amount BIGINT NOT NULL,

    status VARCHAR(20) NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT transfers_amount_positive
        CHECK (amount > 0),

    CONSTRAINT transfers_different_wallets
        CHECK (from_wallet_id <> to_wallet_id),

    CONSTRAINT transfers_valid_status
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),

    CONSTRAINT transfers_from_wallet_fk
        FOREIGN KEY (from_wallet_id)
        REFERENCES wallets(id),

    CONSTRAINT transfers_to_wallet_fk
        FOREIGN KEY (to_wallet_id)
        REFERENCES wallets(id)
);


-- =========================================================
-- LEDGER ENTRIES
-- =========================================================

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    transfer_id UUID NOT NULL,

    wallet_id UUID NOT NULL,

    entry_type VARCHAR(10) NOT NULL,

    amount BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_amount_positive
        CHECK (amount > 0),

    CONSTRAINT ledger_valid_entry_type
        CHECK (entry_type IN ('DEBIT', 'CREDIT')),

    CONSTRAINT ledger_transfer_fk
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CONSTRAINT ledger_wallet_fk
        FOREIGN KEY (wallet_id)
        REFERENCES wallets(id)
);


-- =========================================================
-- IDEMPOTENCY RECORDS
-- =========================================================

CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,

    transfer_id UUID NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT idempotency_transfer_fk
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id)
);


-- =========================================================
-- INDEXES
-- =========================================================

CREATE INDEX idx_transfers_from_wallet
    ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet
    ON transfers(to_wallet_id);

CREATE INDEX idx_ledger_transfer
    ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_wallet
    ON ledger_entries(wallet_id);