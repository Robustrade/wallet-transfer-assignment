CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_ref   TEXT NOT NULL,
    balance     NUMERIC(16,2) NOT NULL DEFAULT 0
                CHECK (balance >= 0 AND balance <= 10000000000000),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          NUMERIC(16,2) NOT NULL
                    CHECK (amount > 0 AND amount <= 10000000000000),
    status          TEXT NOT NULL
                    CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    UUID NOT NULL REFERENCES wallets(id),
    type         TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount       NUMERIC(16,2) NOT NULL
                 CHECK (amount > 0 AND amount <= 10000000000000),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_records (
    idempotency_key  TEXT PRIMARY KEY,
    request_hash     TEXT NOT NULL,
    transfer_id      UUID REFERENCES transfers(id),
    status           TEXT NOT NULL
                     CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_body    JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);