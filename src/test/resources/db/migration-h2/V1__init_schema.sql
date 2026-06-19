-- =============================================================================
-- H2-compatible schema for testing (PostgreSQL compatibility mode)
-- PostgreSQL ENUMs are replaced with VARCHAR + CHECK constraints.
-- JSONB → CLOB, gen_random_uuid() → not used (JPA handles UUID generation)
-- =============================================================================

-- =============================================================================
-- TABLE: wallets
-- =============================================================================
CREATE TABLE wallets (
    id          UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    user_id     UUID NOT NULL,
    balance     DECIMAL(18, 2) NOT NULL DEFAULT 0.00,
    status      VARCHAR(20) NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT balance_non_negative CHECK (balance >= 0),
    CONSTRAINT valid_wallet_status CHECK (status IN ('ACTIVE', 'FROZEN', 'CLOSED'))
);

CREATE INDEX idx_wallets_user_id ON wallets (user_id);

-- =============================================================================
-- TABLE: transfers
-- =============================================================================
CREATE TABLE transfers (
    id                UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    idempotency_key   UUID NOT NULL,
    from_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id      UUID NOT NULL REFERENCES wallets(id),
    amount            DECIMAL(18, 2) NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'COMPLETED',
    created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT no_self_transfer CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT valid_transfer_status CHECK (status IN ('COMPLETED', 'FAILED'))
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id, created_at DESC);

-- =============================================================================
-- TABLE: ledger_entries
-- =============================================================================
CREATE TABLE ledger_entries (
    id              UUID DEFAULT RANDOM_UUID() PRIMARY KEY,
    transfer_id     UUID NOT NULL REFERENCES transfers(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    entry_type      VARCHAR(10) NOT NULL,
    amount          DECIMAL(18, 2) NOT NULL,
    balance_after   DECIMAL(18, 2) NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT debit_must_be_negative  CHECK (entry_type <> 'DEBIT'  OR amount < 0),
    CONSTRAINT credit_must_be_positive CHECK (entry_type <> 'CREDIT' OR amount > 0),
    CONSTRAINT valid_entry_type CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT uq_transfer_wallet_type UNIQUE (transfer_id, wallet_id, entry_type)
);

CREATE INDEX idx_ledger_wallet_created ON ledger_entries (wallet_id, created_at DESC);

-- =============================================================================
-- TABLE: idempotency_records
-- =============================================================================
CREATE TABLE idempotency_records (
    idempotency_key     UUID PRIMARY KEY,
    from_wallet_id      UUID NOT NULL,
    to_wallet_id        UUID NOT NULL,
    amount              DECIMAL(18, 2) NOT NULL,
    transfer_id         UUID REFERENCES transfers(id),
    http_status_code    SMALLINT,
    response_body       CLOB,
    created_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at          TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT DATEADD(HOUR, 24, CURRENT_TIMESTAMP)
);

CREATE INDEX idx_idempotency_expires_at ON idempotency_records (expires_at);
