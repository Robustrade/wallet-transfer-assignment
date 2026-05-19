-- +goose Up
-- Create enums
CREATE TYPE transaction_state AS ENUM ('PENDING','PROCESSED','FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT','CREDIT');

-- wallets table
CREATE TABLE IF NOT EXISTS wallets (
  wallet_id BIGINT PRIMARY KEY,
  balance BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

-- transactions table
CREATE TABLE IF NOT EXISTS transactions (
  id BIGSERIAL PRIMARY KEY,
  idempotency_key TEXT UNIQUE,
  from_wallet_id BIGINT NOT NULL,
  to_wallet_id BIGINT NOT NULL,
  amount BIGINT NOT NULL,
  state transaction_state NOT NULL DEFAULT 'PENDING',
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now(),
  updated_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

-- ledger entries
CREATE TABLE IF NOT EXISTS ledger_entries (
  entry_id BIGSERIAL PRIMARY KEY,
  transaction_id BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
  wallet_id BIGINT NOT NULL,
  type ledger_entry_type NOT NULL,
  amount BIGINT NOT NULL,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet_updated ON ledger_entries (wallet_id, created_at);

ALTER DATABASE wallets SET default_transaction_isolation TO 'read committed';

-- +goose Down
DROP INDEX IF EXISTS idx_ledger_wallet_updated;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transactions;
DROP TABLE IF EXISTS wallets;
DROP TYPE IF EXISTS ledger_entry_type;
DROP TYPE IF EXISTS transaction_state;
