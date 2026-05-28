CREATE TABLE IF NOT EXISTS wallets (
  wallet_id TEXT PRIMARY KEY,
  balance BIGINT NOT NULL CHECK (balance >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
  transfer_id TEXT PRIMARY KEY,
  idempotency_key TEXT NOT NULL UNIQUE,
  from_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
  to_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
  amount BIGINT NOT NULL CHECK (amount > 0),
  state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
  failure_reason TEXT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
  entry_id BIGSERIAL PRIMARY KEY,
  transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id) ON DELETE CASCADE,
  wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
  entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
  amount BIGINT NOT NULL CHECK (amount > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_created_at_idx ON ledger_entries(wallet_id, created_at);
CREATE INDEX IF NOT EXISTS transfers_from_wallet_created_at_idx ON transfers(from_wallet_id, created_at);
CREATE INDEX IF NOT EXISTS transfers_to_wallet_created_at_idx ON transfers(to_wallet_id, created_at);

CREATE TABLE IF NOT EXISTS idempotency_records (
  idempotency_key TEXT PRIMARY KEY,
  request_hash TEXT NOT NULL,
  response_code INT NULL,
  response_body JSONB NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

