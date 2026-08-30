CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,

    idempotency_key TEXT NOT NULL UNIQUE,

    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),

    amount BIGINT NOT NULL CHECK (amount > 0),

    status TEXT NOT NULL CHECK (
        status IN ('PENDING', 'PROCESSED', 'FAILED')
    ),

    request_hash TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,

    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),

    type TEXT NOT NULL CHECK (
        type IN ('DEBIT', 'CREDIT')
    ),

    amount BIGINT NOT NULL CHECK (amount > 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_transfer_id
    ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_entries_wallet_id
    ON ledger_entries(wallet_id);

CREATE INDEX idx_transfers_from_wallet_id
    ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet_id
    ON transfers(to_wallet_id);
