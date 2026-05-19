-- +goose Up
-- idempotency_key UNIQUE is the single DB-level idempotency guarantee.
-- updated_at is set explicitly by the application on every status transition
-- (no DB trigger), per APPROACH.md.
CREATE TABLE transfers (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT          UNIQUE NOT NULL,
    from_wallet_id  TEXT          NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT          NOT NULL REFERENCES wallets(id),
    amount          NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    status          TEXT          NOT NULL DEFAULT 'PENDING'
                                  CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE transfers;
