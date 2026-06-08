CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets (
  id         UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
  balance    NUMERIC(20, 4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
  currency   TEXT           NOT NULL,
  created_at TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');

CREATE TABLE transfers (
  id             UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
  from_wallet_id UUID            NOT NULL REFERENCES wallets(id),
  to_wallet_id   UUID            NOT NULL REFERENCES wallets(id),
  amount         NUMERIC(20, 4)  NOT NULL CHECK (amount > 0),
  status         transfer_status NOT NULL DEFAULT 'PENDING',
  failure_reason TEXT,
  created_at     TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
  processed_at   TIMESTAMPTZ,
  CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);

CREATE TYPE entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE ledger_entries (
  id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
  transfer_id UUID           NOT NULL REFERENCES transfers(id),
  wallet_id   UUID           NOT NULL REFERENCES wallets(id),
  entry_type  entry_type     NOT NULL,
  amount      NUMERIC(20, 4) NOT NULL CHECK (amount > 0),
  created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
  UNIQUE (transfer_id, wallet_id, entry_type)
);

CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id);

CREATE TABLE idempotency_records (
  key             TEXT      PRIMARY KEY,
  request_hash    TEXT      NOT NULL,
  transfer_id     UUID      REFERENCES transfers(id),
  response_status INT       NOT NULL,
  response_body   JSONB     NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
