CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT UNIQUE,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    amount          INTEGER NOT NULL,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT transfers_amount_positive   CHECK (amount > 0),
    CONSTRAINT transfers_different_wallets CHECK (from_wallet_id != to_wallet_id),
    CONSTRAINT transfers_status_valid      CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    entry_type  TEXT NOT NULL,
    amount      INTEGER NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT ledger_entry_type_valid CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_amount_positive  CHECK (amount > 0)
);

CREATE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet     ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet       ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id        ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id          ON ledger_entries(wallet_id);
