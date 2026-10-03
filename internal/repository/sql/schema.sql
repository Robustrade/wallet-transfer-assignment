DO $$
BEGIN
    CREATE TYPE transfer_state AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$
BEGIN
    CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS wallets (
    id              VARCHAR(255) PRIMARY KEY CHECK (id <> ''),
    opening_balance BIGINT       NOT NULL CHECK (opening_balance >= 0),
    balance         BIGINT       NOT NULL CHECK (balance >= 0),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              UUID           PRIMARY KEY,
    idempotency_key VARCHAR(255)   NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    from_wallet_id  VARCHAR(255)   NOT NULL REFERENCES wallets (id),
    to_wallet_id    VARCHAR(255)   NOT NULL REFERENCES wallets (id),
    amount          BIGINT         NOT NULL CHECK (amount > 0),
    state           transfer_state NOT NULL,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGINT            GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID              NOT NULL REFERENCES transfers (id),
    wallet_id   VARCHAR(255)      NOT NULL REFERENCES wallets (id),
    type        ledger_entry_type NOT NULL,
    amount      BIGINT            NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ       NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, type)
);
