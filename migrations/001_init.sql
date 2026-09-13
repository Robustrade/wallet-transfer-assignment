CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    currency TEXT NOT NULL,
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT wallets_id_length CHECK (char_length(id) BETWEEN 1 AND 128),
    CONSTRAINT wallets_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

DO $$
BEGIN
    CREATE TYPE transfer_status AS ENUM (
        'PENDING',
        'PROCESSED',
        'FAILED'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;

DO $$
BEGIN
    CREATE TYPE ledger_entry_type AS ENUM (
        'DEBIT',
        'CREDIT'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END
$$;

CREATE TABLE IF NOT EXISTS transfers (
    id UUID NOT NULL UNIQUE,
    idempotency_key TEXT NOT NULL,
    shard_id SMALLINT NOT NULL,
    request_hash TEXT NOT NULL,

    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL,

    status transfer_status NOT NULL DEFAULT 'PENDING',

    response_code INTEGER NOT NULL DEFAULT 0,
    response_body JSONB NOT NULL DEFAULT '{}'::jsonb,

    failure_code TEXT,
    failure_message TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,

    PRIMARY KEY (idempotency_key, shard_id),

    CONSTRAINT transfers_idempotency_key_length
        CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    CONSTRAINT transfers_shard_id_valid CHECK (shard_id BETWEEN 0 AND 15),

    CONSTRAINT transfers_amount_positive
        CHECK (amount > 0),

    CONSTRAINT transfers_wallets_different
        CHECK (from_wallet_id <> to_wallet_id),

    CONSTRAINT transfers_response_code_valid
        CHECK (
            (status = 'PENDING' AND response_code IN (0, 202))
            OR
            (status <> 'PENDING' AND response_code BETWEEN 200 AND 599)
        ),

    CONSTRAINT transfers_processed_at_valid
        CHECK (
            (status = 'PROCESSED' AND processed_at IS NOT NULL)
            OR
            (status <> 'PROCESSED' AND processed_at IS NULL)
        ),

    CONSTRAINT transfers_failure_fields_valid
        CHECK (
            (status = 'FAILED' AND failure_code IS NOT NULL)
            OR
            (status <> 'FAILED' AND failure_code IS NULL)
        )
);

CREATE INDEX IF NOT EXISTS transfers_from_wallet_idx
    ON transfers(from_wallet_id, created_at DESC);

CREATE INDEX IF NOT EXISTS transfers_to_wallet_idx
    ON transfers(to_wallet_id, created_at DESC);

CREATE INDEX IF NOT EXISTS transfers_status_idx
    ON transfers(status, created_at);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_entries_amount_positive
        CHECK (amount > 0),

    CONSTRAINT ledger_entries_unique_side
        UNIQUE (transfer_id, wallet_id, entry_type)
);

ALTER TYPE ledger_entry_type ADD VALUE IF NOT EXISTS 'COMPENSATING_CREDIT';
ALTER TYPE ledger_entry_type ADD VALUE IF NOT EXISTS 'COMPENSATING_DEBIT';

CREATE TABLE IF NOT EXISTS transfer_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    idempotency_key TEXT NOT NULL,
    shard_id SMALLINT NOT NULL CHECK (shard_id BETWEEN 0 AND 15),
    event_type TEXT NOT NULL CHECK (event_type IN ('DEBIT_WALLET', 'CREDIT_WALLET')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    UNIQUE (transfer_id, event_type),
    UNIQUE (idempotency_key, shard_id, event_type)
);

CREATE INDEX IF NOT EXISTS transfer_events_shard_idx
    ON transfer_events(shard_id, published_at, created_at);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_idx
    ON ledger_entries(wallet_id, created_at DESC);

CREATE INDEX IF NOT EXISTS ledger_entries_transfer_idx
    ON ledger_entries(transfer_id);

CREATE OR REPLACE FUNCTION enforce_transfer_state_transition()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status = NEW.status THEN
        RETURN NEW;
    END IF;

    IF OLD.status = 'PENDING'
       AND NEW.status IN ('PROCESSED', 'FAILED') THEN
        RETURN NEW;
    END IF;

    RAISE EXCEPTION
        'invalid transfer state transition: % -> %',
        OLD.status,
        NEW.status
        USING ERRCODE = '23514';
END;
$$;

DROP TRIGGER IF EXISTS transfer_state_transition_trigger ON transfers;

CREATE TRIGGER transfer_state_transition_trigger
BEFORE UPDATE OF status ON transfers
FOR EACH ROW
EXECUTE FUNCTION enforce_transfer_state_transition();

CREATE OR REPLACE FUNCTION assert_balanced_transfer_ledger()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    target_transfer_id UUID;
    current_status transfer_status;
    transfer_amount BIGINT;

    entry_count INTEGER;
    debit_count INTEGER;
    credit_count INTEGER;

    debit_total BIGINT;
    credit_total BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_transfer_id := OLD.transfer_id;
    ELSE
        target_transfer_id := NEW.transfer_id;
    END IF;

    SELECT status, amount
    INTO current_status, transfer_amount
    FROM transfers
    WHERE id = target_transfer_id;

    IF current_status IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT
        COUNT(*)::INTEGER,
        COUNT(*) FILTER (
            WHERE le.entry_type = 'DEBIT'
              AND le.wallet_id = t.from_wallet_id
              AND le.amount = t.amount
        )::INTEGER,
        COUNT(*) FILTER (
            WHERE le.entry_type = 'CREDIT'
              AND le.wallet_id = t.to_wallet_id
              AND le.amount = t.amount
        )::INTEGER,
        COALESCE(SUM(le.amount) FILTER (
            WHERE le.entry_type = 'DEBIT'
        ), 0),
        COALESCE(SUM(le.amount) FILTER (
            WHERE le.entry_type = 'CREDIT'
        ), 0)
    INTO
        entry_count,
        debit_count,
        credit_count,
        debit_total,
        credit_total
    FROM ledger_entries le
    JOIN transfers t ON t.id = le.transfer_id
    WHERE le.transfer_id = target_transfer_id;

    IF current_status = 'PROCESSED' THEN
        IF entry_count <> 2
           OR debit_count <> 1
           OR credit_count <> 1
           OR debit_total <> credit_total
           OR debit_total <> transfer_amount THEN
            RAISE EXCEPTION
                'transfer % does not have a valid double-entry ledger',
                target_transfer_id
                USING ERRCODE = '23514';
        END IF;
    ELSIF current_status = 'PENDING' THEN
        IF entry_count > 1 OR debit_count > 1 THEN
            RAISE EXCEPTION
                'pending transfer % has invalid debit entries',
                target_transfer_id
                USING ERRCODE = '23514';
        END IF;
    ELSE
        IF entry_count > 4 THEN
            RAISE EXCEPTION
                'failed transfer % has too many ledger entries',
                target_transfer_id
                USING ERRCODE = '23514';
        END IF;
    END IF;

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS ledger_entries_balance_check ON ledger_entries;

CREATE CONSTRAINT TRIGGER ledger_entries_balance_check
AFTER INSERT OR UPDATE OR DELETE ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION assert_balanced_transfer_ledger();

DROP TRIGGER IF EXISTS processed_transfer_balance_check ON transfers;

CREATE CONSTRAINT TRIGGER processed_transfer_balance_check
AFTER INSERT OR UPDATE OF status ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION assert_balanced_transfer_ledger();