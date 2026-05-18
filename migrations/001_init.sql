-- =============================================================
-- Wallet Transfer — Initial Schema (SQLite)
-- =============================================================

CREATE TABLE IF NOT EXISTS wallets (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL,
    currency   TEXT NOT NULL DEFAULT 'USD',
    balance    TEXT NOT NULL DEFAULT '0.00000000',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    -- one wallet per user per currency
    UNIQUE (user_id, currency),
    CONSTRAINT chk_balance_non_negative CHECK (CAST(balance AS NUMERIC) >= 0)
);

CREATE TABLE IF NOT EXISTS transactions (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key TEXT NOT NULL UNIQUE,
    reference_id    TEXT NOT NULL UNIQUE,
    from_wallet_id  INTEGER NOT NULL,
    to_wallet_id    INTEGER NOT NULL,
    amount          TEXT NOT NULL,
    currency        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'PENDING',
    description     TEXT,
    metadata        TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_txn_from FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),
    CONSTRAINT fk_txn_to   FOREIGN KEY (to_wallet_id)   REFERENCES wallets(id),
    CONSTRAINT chk_amount_positive CHECK (CAST(amount AS NUMERIC) > 0),
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id != to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id INTEGER NOT NULL,
    wallet_id      INTEGER NOT NULL,
    entry_type     TEXT NOT NULL,
    amount         TEXT NOT NULL,
    balance_before TEXT NOT NULL,
    balance_after  TEXT NOT NULL,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_ledger_txn    FOREIGN KEY (transaction_id) REFERENCES transactions(id),
    CONSTRAINT fk_ledger_wallet FOREIGN KEY (wallet_id)      REFERENCES wallets(id),
    CONSTRAINT chk_entry_type   CHECK (entry_type IN ('debit', 'credit')),
    CONSTRAINT chk_ledger_amount_positive CHECK (CAST(amount AS NUMERIC) > 0)
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    payload    TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'PENDING',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries(wallet_id, created_at);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id_desc ON ledger_entries(wallet_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_txn ON ledger_entries(transaction_id);
