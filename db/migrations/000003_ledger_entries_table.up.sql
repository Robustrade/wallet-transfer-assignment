CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(40) PRIMARY KEY,
    transfer_id VARCHAR(40) NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR(40) NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(transfer_id, wallet_id, entry_type)
);