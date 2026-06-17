-- Schema for the wallet transfer service.
-- Written idempotently so it can be applied safely on every startup and in tests.

-- Money is stored as BIGINT minor units (e.g. cents) -- never floats.

DO $$ BEGIN
  CREATE TYPE transfer_status AS ENUM ('PENDING','PROCESSED','FAILED');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
  CREATE TYPE entry_type AS ENUM ('DEBIT','CREDIT');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE TABLE IF NOT EXISTS wallets (
  id         TEXT PRIMARY KEY,
  balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),   -- no overdraft, ever
  currency   TEXT   NOT NULL DEFAULT 'USD',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
  to_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
  amount         BIGINT NOT NULL CHECK (amount > 0),
  status         transfer_status NOT NULL DEFAULT 'PENDING',
  failure_reason TEXT,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (from_wallet_id <> to_wallet_id)                       -- no self-transfer
);

CREATE TABLE IF NOT EXISTS ledger_entries (
  id          BIGSERIAL PRIMARY KEY,
  transfer_id UUID NOT NULL REFERENCES transfers(id),
  wallet_id   TEXT NOT NULL REFERENCES wallets(id),
  type        entry_type NOT NULL,
  amount      BIGINT NOT NULL CHECK (amount > 0),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
  idempotency_key TEXT PRIMARY KEY,
  request_hash    TEXT NOT NULL,
  transfer_id     UUID REFERENCES transfers(id),
  status          TEXT NOT NULL,            -- IN_PROGRESS | COMPLETED
  response_status INT,
  response_body   JSONB,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backstop: a deferred constraint trigger asserting the ledger balances per transfer.
-- Fires at COMMIT, so all entries for a transfer are present when it checks.
-- A FAILED transfer has zero entries, so the trigger never fires for it.
CREATE OR REPLACE FUNCTION assert_transfer_balanced() RETURNS trigger AS $$
DECLARE
  net BIGINT;
BEGIN
  SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
    INTO net
    FROM ledger_entries
   WHERE transfer_id = NEW.transfer_id;
  IF net <> 0 THEN
    RAISE EXCEPTION 'ledger not balanced for transfer %: net=%', NEW.transfer_id, net;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_ledger_balanced ON ledger_entries;
CREATE CONSTRAINT TRIGGER trg_ledger_balanced
  AFTER INSERT ON ledger_entries
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_transfer_balanced();
