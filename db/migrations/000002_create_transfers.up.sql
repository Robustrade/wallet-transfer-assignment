CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');

CREATE TABLE transfers (
    id              UUID            NOT NULL DEFAULT gen_random_uuid(),
    idempotency_key TEXT            NOT NULL,
    from_wallet_id  TEXT            NOT NULL,
    to_wallet_id    TEXT            NOT NULL,
    amount          BIGINT          NOT NULL,
    status          transfer_status NOT NULL DEFAULT 'PENDING',
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT transfers_pkey             PRIMARY KEY (id),
    CONSTRAINT transfers_idempotency_key  UNIQUE (idempotency_key),
    CONSTRAINT transfers_from_wallet_fk   FOREIGN KEY (from_wallet_id) REFERENCES wallets (id),
    CONSTRAINT transfers_to_wallet_fk     FOREIGN KEY (to_wallet_id)   REFERENCES wallets (id),
    CONSTRAINT transfers_amount_positive  CHECK (amount > 0),
    CONSTRAINT transfers_wallets_differ   CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id);
