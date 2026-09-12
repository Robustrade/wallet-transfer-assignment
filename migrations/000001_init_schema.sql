-- Migration 000001: Initial schema for Wallet Transfer Service

CREATE TABLE IF NOT EXISTS wallets (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transfers (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(32) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT check_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type VARCHAR(16) NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE ledger_entries ALTER COLUMN transfer_id DROP NOT NULL;
ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS check_null_transfer_credit;
ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS check_ledger_transfer_id;
ALTER TABLE ledger_entries ADD CONSTRAINT check_ledger_transfer_id CHECK (
    transfer_id IS NOT NULL OR (id = 'entry_system_treasury_opening' AND wallet_id = 'system_treasury' AND type = 'CREDIT')
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(128) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    owner_token VARCHAR(64) NOT NULL DEFAULT '',
    transfer_id VARCHAR(64) REFERENCES transfers(id),
    status VARCHAR(32) NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_code INT,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE idempotency_records ADD COLUMN IF NOT EXISTS owner_token VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_entries_transfer_type ON ledger_entries (transfer_id, type) WHERE transfer_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ledger_entries_transfer_wallet ON ledger_entries (transfer_id, wallet_id) WHERE transfer_id IS NOT NULL;

-- System Treasury wallet for double-entry initial deposits
INSERT INTO wallets (id, name, balance, currency)
VALUES ('system_treasury', 'System Treasury', 100000000000000, 'USD')
ON CONFLICT (id) DO NOTHING;

-- Ledger opening funding record for system_treasury so stored balance reconciles with ledger
INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
VALUES ('entry_system_treasury_opening', NULL, 'system_treasury', 'CREDIT', 100000000000000)
ON CONFLICT (id) DO NOTHING;

-- Database-level constraint trigger on ledger_entries enforcing that every transfer has exactly two balanced entries:
-- exactly one DEBIT on from_wallet_id and one CREDIT on to_wallet_id, matching transfer amount.
CREATE OR REPLACE FUNCTION check_ledger_pair_integrity()
RETURNS TRIGGER AS $$
DECLARE
    transfer_rec RECORD;
    debit_count INT;
    credit_count INT;
    debit_sum BIGINT;
    credit_sum BIGINT;
BEGIN
    IF NEW.transfer_id IS NOT NULL THEN
        SELECT * INTO transfer_rec FROM transfers WHERE id = NEW.transfer_id;
        IF FOUND THEN
            IF transfer_rec.status <> 'PROCESSED' THEN
                RAISE EXCEPTION 'invalid ledger pair for transfer %: transfer status must be PROCESSED at commit time, got %', NEW.transfer_id, transfer_rec.status;
            END IF;

            SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO debit_count, debit_sum
            FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id AND type = 'DEBIT' AND wallet_id = transfer_rec.from_wallet_id;

            SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO credit_count, credit_sum
            FROM ledger_entries
            WHERE transfer_id = NEW.transfer_id AND type = 'CREDIT' AND wallet_id = transfer_rec.to_wallet_id;

            IF debit_count <> 1 OR credit_count <> 1 OR debit_sum <> transfer_rec.amount OR credit_sum <> transfer_rec.amount THEN
                RAISE EXCEPTION 'invalid ledger pair for transfer %: must have exactly 1 DEBIT on from_wallet and 1 CREDIT on to_wallet matching transfer amount', NEW.transfer_id;
            END IF;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_check_ledger_pair ON ledger_entries;
CREATE CONSTRAINT TRIGGER trg_check_ledger_pair
AFTER INSERT OR UPDATE ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_ledger_pair_integrity();

-- Database-level constraint trigger on transfers enforcing that no transfer can be committed as PROCESSED
-- without an exact, balanced double-entry pair existing in ledger_entries, and no FAILED transfer can have ledger entries.
CREATE OR REPLACE FUNCTION check_transfer_processed_ledger_pair()
RETURNS TRIGGER AS $$
DECLARE
    current_status VARCHAR(20);
    debit_count INT;
    credit_count INT;
    debit_sum BIGINT;
    credit_sum BIGINT;
BEGIN
    -- Reject any updates to terminal transfers at the database boundary (completely immutable)
    IF TG_OP = 'UPDATE' AND OLD.status IN ('PROCESSED', 'FAILED') THEN
        RAISE EXCEPTION 'terminal transfer % (status: %) is immutable and cannot be updated', OLD.id, OLD.status;
    END IF;

    -- Look up the actual commit-time status of the transfer
    SELECT status INTO current_status FROM transfers WHERE id = NEW.id;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF current_status = 'PROCESSED' THEN
        SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO debit_count, debit_sum
        FROM ledger_entries
        WHERE transfer_id = NEW.id AND type = 'DEBIT' AND wallet_id = NEW.from_wallet_id;

        SELECT COUNT(*), COALESCE(SUM(amount), 0) INTO credit_count, credit_sum
        FROM ledger_entries
        WHERE transfer_id = NEW.id AND type = 'CREDIT' AND wallet_id = NEW.to_wallet_id;

        IF debit_count <> 1 OR credit_count <> 1 OR debit_sum <> NEW.amount OR credit_sum <> NEW.amount THEN
            RAISE EXCEPTION 'invalid ledger pair for transfer %: cannot commit PROCESSED transfer: requires exactly 1 DEBIT on from_wallet (%) and 1 CREDIT on to_wallet (%) matching transfer amount (%)',
                NEW.id, NEW.from_wallet_id, NEW.to_wallet_id, NEW.amount;
        END IF;
    ELSIF current_status = 'FAILED' THEN
        SELECT COUNT(*) INTO debit_count
        FROM ledger_entries
        WHERE transfer_id = NEW.id;

        IF debit_count > 0 THEN
            RAISE EXCEPTION 'cannot commit FAILED transfer % with existing ledger entries', NEW.id;
        END IF;
    ELSE
        -- Non-terminal status (such as PENDING) must not have ledger entries attached at commit
        SELECT COUNT(*) INTO debit_count
        FROM ledger_entries
        WHERE transfer_id = NEW.id;

        IF debit_count > 0 THEN
            RAISE EXCEPTION 'cannot commit non-terminal transfer % (status: %) with existing ledger entries', NEW.id, current_status;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_check_transfer_processed ON transfers;
CREATE CONSTRAINT TRIGGER trg_check_transfer_processed
AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION check_transfer_processed_ledger_pair();

-- Immutability trigger on ledger_entries: prevent updates and deletes to protect the audit trail
CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable: deletions and updates are forbidden';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_prevent_ledger_mutation ON ledger_entries;
CREATE TRIGGER trg_prevent_ledger_mutation
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_ledger_mutation();

DROP TRIGGER IF EXISTS trg_prevent_ledger_truncate ON ledger_entries;
CREATE TRIGGER trg_prevent_ledger_truncate
BEFORE TRUNCATE ON ledger_entries
FOR EACH STATEMENT
EXECUTE FUNCTION prevent_ledger_mutation();

-- Ensure least-privileged runtime application role has only required DML if it exists
DO $$
BEGIN
    IF EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'wallet_app') THEN
        GRANT USAGE ON SCHEMA public TO wallet_app;
        GRANT SELECT, INSERT, UPDATE ON TABLE wallets, transfers TO wallet_app;
        GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE idempotency_records TO wallet_app;
        GRANT SELECT, INSERT ON TABLE ledger_entries TO wallet_app;
        GRANT SELECT ON TABLE schema_migrations TO wallet_app;
        GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO wallet_app;
        GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO wallet_app;

        -- Explicitly revoke destructive privileges on ledger_entries and tables from runtime role
        REVOKE UPDATE, DELETE, TRUNCATE ON TABLE ledger_entries FROM wallet_app;
        REVOKE TRUNCATE ON ALL TABLES IN SCHEMA public FROM wallet_app;
    END IF;
EXCEPTION WHEN insufficient_privilege THEN
    NULL;
END $$;
