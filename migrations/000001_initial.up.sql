BEGIN;

CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT wallets_balance_nonnegative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    to_wallet_id TEXT NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_key_nonempty CHECK (idempotency_key IS NULL OR length(idempotency_key) > 0),
    -- Supports a composite FK that prevents a record referencing the wrong key.
    CONSTRAINT transfers_id_key_unique UNIQUE (id, idempotency_key)
);

CREATE TABLE ledger_entries (
    id TEXT PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    transfer_id TEXT NOT NULL REFERENCES transfers (id) ON DELETE RESTRICT,
    entry_type TEXT NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_entry_type_valid CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_transfer_type_unique UNIQUE (transfer_id, entry_type)
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_fingerprint TEXT NOT NULL,
    transfer_id TEXT NOT NULL UNIQUE,
    -- Populated together before committing a terminal result for exact replay.
    response_status INTEGER,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT idempotency_key_nonempty CHECK (length(idempotency_key) > 0),
    CONSTRAINT idempotency_fingerprint_sha256 CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT idempotency_response_pair CHECK (
        (response_status IS NULL AND response_body IS NULL) OR
        (response_status IS NOT NULL AND response_status BETWEEN 100 AND 599 AND response_body IS NOT NULL)
    ),
    -- Deferred so the unique key can be claimed before inserting the transfer.
    CONSTRAINT idempotency_transfer_key_fk FOREIGN KEY (transfer_id, idempotency_key)
        REFERENCES transfers (id, idempotency_key) DEFERRABLE INITIALLY DEFERRED
);

-- Every keyed transfer must have a record by commit. Both inserts share one tx.
ALTER TABLE transfers ADD CONSTRAINT transfers_idempotency_record_fk
    FOREIGN KEY (idempotency_key) REFERENCES idempotency_records (idempotency_key)
    DEFERRABLE INITIALLY DEFERRED;

COMMENT ON TABLE ledger_entries IS
    'Only PROCESSED transfers represent money movement: exactly one DEBIT and one CREDIT. FAILED transfers have no entries. Service enforces cross-row invariants atomically.';

COMMIT;
