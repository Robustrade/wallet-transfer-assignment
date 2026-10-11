CREATE TABLE wallets (
    id          text PRIMARY KEY CHECK (length(id) > 0),
    balance     bigint NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id                uuid PRIMARY KEY,
    from_wallet_id    text NOT NULL REFERENCES wallets(id),
    to_wallet_id      text NOT NULL REFERENCES wallets(id),
    amount            bigint NOT NULL CHECK (amount > 0),
    state             text NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_code      text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK (
        (state = 'FAILED' AND failure_code IS NOT NULL)
        OR (state IN ('PENDING', 'PROCESSED') AND failure_code IS NULL)
    ),
    CHECK (
        (state = 'PENDING' AND completed_at IS NULL)
        OR (state IN ('PROCESSED', 'FAILED') AND completed_at IS NOT NULL)
    )
);

CREATE INDEX transfers_from_wallet_created_idx
    ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx
    ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id  uuid NOT NULL REFERENCES transfers(id),
    wallet_id    text NOT NULL REFERENCES wallets(id),
    entry_type   text NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       bigint NOT NULL CHECK (amount > 0),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_created_idx
    ON ledger_entries (wallet_id, created_at DESC);

CREATE TABLE idempotency_records (
    idempotency_key  text PRIMARY KEY CHECK (length(idempotency_key) > 0),
    request_hash     bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    state            text NOT NULL CHECK (state IN ('IN_PROGRESS', 'COMPLETED')),
    transfer_id      uuid UNIQUE REFERENCES transfers(id),
    http_status      smallint,
    response_body    jsonb,
    created_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz,
    CHECK (
        (state = 'IN_PROGRESS'
            AND transfer_id IS NULL
            AND http_status IS NULL
            AND response_body IS NULL
            AND completed_at IS NULL)
        OR
        (state = 'COMPLETED'
            AND http_status IS NOT NULL
            AND http_status BETWEEN 100 AND 599
            AND response_body IS NOT NULL
            AND completed_at IS NOT NULL)
    )
);
