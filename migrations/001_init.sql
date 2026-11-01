CREATE TABLE wallets (
    id TEXT PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 64),
    balance BIGINT NOT NULL CHECK (balance >= 0),
    initial_balance BIGINT NOT NULL CHECK (initial_balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    from_wallet_id TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets (id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL)
        OR (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

CREATE TABLE ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets (id),
    transfer_id UUID NOT NULL REFERENCES transfers (id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id, id);
CREATE INDEX transfers_from_wallet_idx ON transfers (from_wallet_id, created_at);
CREATE INDEX transfers_to_wallet_idx ON transfers (to_wallet_id, created_at);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    request_hash TEXT NOT NULL,
    transfer_id UUID REFERENCES transfers (id),
    response_status INT,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (response_status IS NULL AND response_body IS NULL)
        OR (response_status IS NOT NULL AND response_body IS NOT NULL)
    )
);
