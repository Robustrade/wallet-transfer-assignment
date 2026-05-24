-- Wallet Transfer Service — Initial Schema
-- Uses NUMERIC(19,4) for all monetary values to avoid floating-point errors.

CREATE TABLE IF NOT EXISTS wallets (
  id         TEXT PRIMARY KEY,
  balance    NUMERIC(19, 4) NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ    NOT NULL DEFAULT NOW(),

  CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS transfers (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  idempotency_key TEXT         NOT NULL,
  from_wallet_id  TEXT         NOT NULL REFERENCES wallets(id),
  to_wallet_id    TEXT         NOT NULL REFERENCES wallets(id),
  amount          NUMERIC(19, 4) NOT NULL,
  status          TEXT         NOT NULL DEFAULT 'PENDING',
  failure_reason  TEXT,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

  -- UNIQUE on idempotency_key is the primary deduplication mechanism
  CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
  CONSTRAINT transfers_amount_positive        CHECK  (amount > 0),
  CONSTRAINT transfers_status_valid           CHECK  (status IN ('PENDING', 'PROCESSED', 'FAILED')),
  CONSTRAINT transfers_different_wallets      CHECK  (from_wallet_id != to_wallet_id)
);

-- Every transfer produces exactly two ledger entries: one DEBIT, one CREDIT.
CREATE TABLE IF NOT EXISTS ledger_entries (
  id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  wallet_id   TEXT         NOT NULL REFERENCES wallets(id),
  transfer_id UUID         NOT NULL REFERENCES transfers(id),
  type        TEXT         NOT NULL,
  amount      NUMERIC(19, 4) NOT NULL,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

  CONSTRAINT ledger_entries_type_valid     CHECK (type IN ('DEBIT', 'CREDIT')),
  CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet      ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet        ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id   ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
