-- ============================================================================
-- Wallet & Transfer Service - Fresh Database Migration
-- ============================================================================

-- 1. Create Custom ENUM Types
DO $$ BEGIN
    CREATE TYPE wallet_status AS ENUM (
        'ACTIVE', 
        'FROZEN', 
        'DEBIT_FROZEN', 
        'CREDIT_FROZEN',
        'CLOSED'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE transfer_status AS ENUM (
        'PENDING', 
        'PROCESSED', 
        'FAILED'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE ledger_entry_type AS ENUM (
        'DEBIT', 
        'CREDIT'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE idempotency_status AS ENUM (
        'STARTED', 
        'COMPLETED', 
        'FAILED', 
        'EXPIRED'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- 2. Timestamp Trigger Function for Auto-Updating updated_at
CREATE OR REPLACE FUNCTION set_updated_at_timestamp()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 3. Wallets Table
CREATE TABLE IF NOT EXISTS wallets (
    id VARCHAR(64) PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    status wallet_status NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TRIGGER IF EXISTS trg_wallets_updated_at ON wallets;
CREATE TRIGGER trg_wallets_updated_at
BEFORE UPDATE ON wallets
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_timestamp();

-- 4. Transfers Table
CREATE TABLE IF NOT EXISTS transfers (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status transfer_status NOT NULL DEFAULT 'PENDING',
    failure_reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

DROP TRIGGER IF EXISTS trg_transfers_updated_at ON transfers;
CREATE TRIGGER trg_transfers_updated_at
BEFORE UPDATE ON transfers
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_timestamp();

-- 5. Double-Entry Immutable Ledger Entries Table
CREATE TABLE IF NOT EXISTS ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id),
    type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    balance_before BIGINT NOT NULL CHECK (balance_before >= 0),
    balance_after BIGINT NOT NULL CHECK (balance_after >= 0),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_transfer_wallet_type UNIQUE (transfer_id, wallet_id, type)
);

-- 6. Idempotency Records Table
CREATE TABLE IF NOT EXISTS idempotency_records (
    key VARCHAR(128) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    status idempotency_status NOT NULL DEFAULT 'STARTED',
    response_code INT,
    response_body TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

DROP TRIGGER IF EXISTS trg_idempotency_records_updated_at ON idempotency_records;
CREATE TRIGGER trg_idempotency_records_updated_at
BEFORE UPDATE ON idempotency_records
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_timestamp();

-- 7. Optimized Performance Indexes
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);

-- Composite index for fast chronological statements without in-memory sort
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_created_desc ON ledger_entries(wallet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries(transfer_id);

-- Composite index for TTL expiration and lease checks
CREATE INDEX IF NOT EXISTS idx_idempotency_status_updated ON idempotency_records(status, updated_at);