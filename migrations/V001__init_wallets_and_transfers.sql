-- =====================================================================
-- V001: Initialize Wallets and Transfers Tables
-- =====================================================================
-- This is the core schema for the wallet transfer platform.
--
-- Design Principles:
-- 1. Use BIGINT for monetary amounts (no floating-point precision loss)
-- 2. Store balance separately from ledger for fast wallet queries
-- 3. Use version column for optimistic locking / concurrency control
-- 4. Use TIMESTAMPTZ for audit trail (timezone-aware timestamps)
-- 5. Use BIGINT default 0 to represent 0 in smallest currency unit (cents)
-- =====================================================================

-- Enable pgcrypto for UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- =====================================================================
-- ENUMS
-- =====================================================================

-- Transfer status progression: PENDING -> PROCESSED (success) or FAILED
CREATE TYPE transfer_status AS ENUM (
    'PENDING',      -- Transfer initiated, not yet executed
    'PROCESSED',    -- Debit and credit ledger entries created, completed
    'FAILED'        -- Transfer failed (e.g., insufficient funds)
);

-- =====================================================================
-- WALLETS TABLE
-- =====================================================================
-- Purpose:
--   Store current wallet state for fast balance queries.
--   Balance is duplicated from ledger for read performance.
--
-- Design Decisions:
--
-- BALANCE as BIGINT (not FLOAT):
--   - Floating-point arithmetic has precision loss (e.g., 0.1 + 0.2 != 0.3)
--   - BIGINT stores cents (or smallest unit) exactly: 100 = $1.00, 1000 = $10.00
--   - Enables exact comparisons and additions without rounding errors
--   - Convention: 1 BIGINT unit = 1 cent (1/100 of primary currency unit)
--   - To scale to other units: multiply by 100^N in application layer
--
-- BALANCE STORED SEPARATELY FROM LEDGER:
--   - Quick lookup: SELECT balance FROM wallets WHERE id = ? (O(1) index lookup)
--   - Without separate balance: must sum entire ledger (O(N) table scan)
--   - Ledger remains authoritative for auditability
--   - Balance reconciliation: periodic verification task compares balance vs sum(ledger)
--   - Pattern: "Stored aggregate + Transaction log" for both performance and auditability
--
-- PREVENTING NEGATIVE BALANCES:
--   - Check constraint: balance >= 0 prevents direct negative updates
--   - Debit operation in transfer_service checks balance before executing
--   - Double-entry ledger ensures balance integrity (every debit has matching credit)
--   - Combination prevents partial failures and maintains invariants
--
-- VERSION COLUMN FOR OPTIMISTIC LOCKING:
--   - Used by application layer for concurrent update detection
--   - Updated on every wallet change
--   - Pattern: "If version = expected_version THEN update AND increment version"
--   - Enables lock-free concurrency for reads
--   - Fails gracefully if another transaction modified the wallet
--
CREATE TABLE wallets (
    -- Primary Key: UUID for distributed database readiness
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Current Balance: BIGINT in smallest currency unit (e.g., cents)
    -- Constraint ensures non-negative value
    balance BIGINT NOT NULL DEFAULT 0
        CONSTRAINT chk_wallet_balance_non_negative CHECK (balance >= 0),

    -- Version for optimistic locking
    -- Incremented on every update to detect concurrent modifications
    version BIGINT NOT NULL DEFAULT 0
        CONSTRAINT chk_wallet_version_non_negative CHECK (version >= 0),

    -- Audit Columns
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Composite Check: Ensure consistency
    CONSTRAINT chk_wallet_timestamps
        CHECK (created_at <= updated_at)
);

-- Indexes on Wallets
-- Primary key already provides index on (id)
-- Optional: Add index on created_at for time-range queries (e.g., reconciliation)
CREATE INDEX idx_wallet_created_at
    ON wallets(created_at DESC);

-- =====================================================================
-- TRANSFERS TABLE
-- =====================================================================
-- Purpose:
--   Track transfer requests and their status.
--   Central record of all money movement.
--
-- Design Decisions:
--
-- IDEMPOTENCY KEY as UNIQUE:
--   - Prevents duplicate transfers from multiple identical requests
--   - Database constraint (UNIQUE) ensures deduplication at storage layer
--   - API can safely retry requests without manual deduplication logic
--   - Pattern: "Request Idempotency" using natural unique identifier
--
-- AMOUNT as BIGINT:
--   - Same rationale as wallet balance: exact arithmetic
--   - Always positive (checked via constraint)
--   - Prevents accidental zero or negative transfers
--
-- FOREIGN KEYS WITH CASCADING:
--   - from_wallet_id, to_wallet_id reference wallets(id)
--   - No CASCADE DELETE on from/to wallets (data integrity)
--   - On wallet deletion, transfers remain (audit trail preserved)
--   - Transfer query: find all transfers for a wallet (uses index)
--
-- STATUS AS ENUM:
--   - Constraint at database level (only valid statuses allowed)
--   - Enum values: PENDING (transient), PROCESSED (success), FAILED (failure)
--   - Prevents invalid state transitions at storage layer
--
-- FAILURE REASON (nullable TEXT):
--   - Stores error message if transfer fails (e.g., "Insufficient funds")
--   - Helps debugging and customer support
--   - Only populated for FAILED transfers
--
CREATE TABLE transfers (
    -- Primary Key: UUID
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Idempotency Key: Unique request identifier
    -- Enables idempotent API calls; safe to retry
    idempotency_key TEXT NOT NULL
        CONSTRAINT chk_idempotency_key_not_empty CHECK (length(idempotency_key) > 0),
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),

    -- Wallet References
    from_wallet_id UUID NOT NULL REFERENCES wallets(id)
        ON DELETE RESTRICT,  -- Prevent deleting wallet if transfers exist
    to_wallet_id UUID NOT NULL REFERENCES wallets(id)
        ON DELETE RESTRICT,

    -- Transfer Amount: BIGINT in smallest currency unit
    amount BIGINT NOT NULL
        CONSTRAINT chk_transfer_amount_positive CHECK (amount > 0),

    -- Transfer Status: ENUM to restrict valid states
    status transfer_status NOT NULL DEFAULT 'PENDING',

    -- Failure Reason: Optional explanation if status = FAILED
    failure_reason TEXT
        CONSTRAINT chk_failure_reason_not_empty CHECK (failure_reason IS NULL OR length(failure_reason) > 0),

    -- Audit Columns
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints
    CONSTRAINT chk_transfer_different_wallets
        CHECK (from_wallet_id <> to_wallet_id),  -- Prevent self-transfers
    CONSTRAINT chk_transfer_timestamps
        CHECK (created_at <= updated_at),
    CONSTRAINT chk_transfer_failure_reason
        -- Consistency: FAILED status must have a reason
        CHECK ((status = 'FAILED' AND failure_reason IS NOT NULL) OR status <> 'FAILED')
);

-- Indexes on Transfers
CREATE INDEX idx_transfer_from_wallet
    ON transfers(from_wallet_id)
    WHERE status = 'PROCESSED';  -- Filter index for completed transfers

CREATE INDEX idx_transfer_to_wallet
    ON transfers(to_wallet_id)
    WHERE status = 'PROCESSED';

-- Index already exists on idempotency_key via UNIQUE constraint
-- Additional non-unique index not needed

CREATE INDEX idx_transfer_created_at
    ON transfers(created_at DESC);  -- For time-range queries

CREATE INDEX idx_transfer_status
    ON transfers(status)
    WHERE status IN ('PENDING', 'FAILED');  -- For monitoring/reconciliation

-- =====================================================================
-- AUDIT AND MONITORING
-- =====================================================================

-- Optional: Trigger to automatically update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql
IMMUTABLE;

CREATE TRIGGER trg_wallets_updated_at
    BEFORE UPDATE ON wallets
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_transfers_updated_at
    BEFORE UPDATE ON transfers
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
