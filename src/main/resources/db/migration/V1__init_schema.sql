-- =============================================================================
-- ENUM TYPES
-- =============================================================================
CREATE TYPE wallet_status     AS ENUM ('ACTIVE', 'FROZEN', 'CLOSED');
CREATE TYPE transfer_status   AS ENUM ('COMPLETED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

-- =============================================================================
-- TABLE: wallets
-- =============================================================================
CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL,
    balance     DECIMAL(18, 2) NOT NULL DEFAULT 0.00,
    status      wallet_status NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT balance_non_negative CHECK (balance >= 0)
);

CREATE INDEX idx_wallets_user_id ON wallets (user_id);

-- =============================================================================
-- TABLE: transfers
-- =============================================================================
CREATE TABLE transfers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key   UUID NOT NULL,
    from_wallet_id    UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id      UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount            DECIMAL(18, 2) NOT NULL,
    status            transfer_status NOT NULL DEFAULT 'COMPLETED',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT no_self_transfer CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id, created_at DESC);

-- =============================================================================
-- TABLE: ledger_entries
-- =============================================================================
CREATE TABLE ledger_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id     UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id       UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    entry_type      ledger_entry_type NOT NULL,
    amount          DECIMAL(18, 2) NOT NULL,
    balance_after   DECIMAL(18, 2) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT debit_must_be_negative  CHECK (entry_type <> 'DEBIT'  OR amount < 0),
    CONSTRAINT credit_must_be_positive CHECK (entry_type <> 'CREDIT' OR amount > 0),
    CONSTRAINT uq_transfer_wallet_type UNIQUE (transfer_id, wallet_id, entry_type)
);

CREATE INDEX idx_ledger_wallet_created
    ON ledger_entries (wallet_id, created_at DESC);

-- =============================================================================
-- TABLE: idempotency_records
-- =============================================================================
CREATE TABLE idempotency_records (
    idempotency_key     UUID PRIMARY KEY,
    from_wallet_id      UUID NOT NULL,
    to_wallet_id        UUID NOT NULL,
    amount              DECIMAL(18, 2) NOT NULL,
    transfer_id         UUID REFERENCES transfers(id) ON DELETE RESTRICT,
    http_status_code    SMALLINT,
    response_body       JSONB,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '24 hours')
);

CREATE INDEX idx_idempotency_expires_at ON idempotency_records (expires_at);
