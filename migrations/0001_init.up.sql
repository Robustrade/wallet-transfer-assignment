CREATE TABLE wallets (
  id          TEXT PRIMARY KEY,
  balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');

CREATE TABLE transfers (
  id              UUID PRIMARY KEY,
  from_wallet_id  TEXT NOT NULL REFERENCES wallets(id),
  to_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
  amount          BIGINT NOT NULL CHECK (amount > 0),
  status          transfer_status NOT NULL DEFAULT 'PENDING',
  failure_reason  TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE ledger_entries (
  id           BIGSERIAL PRIMARY KEY,
  transfer_id  UUID NOT NULL REFERENCES transfers(id),
  wallet_id    TEXT NOT NULL REFERENCES wallets(id),
  entry_type   ledger_entry_type NOT NULL,
  amount       BIGINT NOT NULL CHECK (amount > 0),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (transfer_id, entry_type)
);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer_id ON ledger_entries(transfer_id);

CREATE TYPE idempotency_status AS ENUM ('IN_PROGRESS', 'COMPLETED');

CREATE TABLE idempotency_records (
  idempotency_key      TEXT PRIMARY KEY,
  request_fingerprint  TEXT NOT NULL,
  status               idempotency_status NOT NULL DEFAULT 'IN_PROGRESS',
  transfer_id          UUID REFERENCES transfers(id),
  response_status      INT,
  response_body        JSON,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (
    (status = 'IN_PROGRESS' AND response_status IS NULL AND response_body IS NULL)
    OR
    (status = 'COMPLETED' AND response_status IS NOT NULL AND response_body IS NOT NULL)
  )
);
