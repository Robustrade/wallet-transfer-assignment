

CREATE TABLE wallet (
    wallet_id TEXT PRIMARY KEY,
    balance INTEGER NOT NULL CHECK (balance >= 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP

);

CREATE TABLE transfer (
    transaction_id TEXT PRIMARY KEY,
    from_wallet_id TEXT references wallet(wallet_id) NOT NULL,
    to_wallet_id TEXT references wallet(wallet_id) NOT NULL,
    amount INTEGER NOT NULL CHECK ( amount>0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    comments TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    check(from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entry(
    entry_id TEXT PRIMARY KEY,
    wallet_id TEXT references wallet(wallet_id) NOT NULL,
    transaction_id TEXT references transfer(transaction_id) not NULL,
    amount INTEGER not null check ( amount>0),
    balance_after INTEGER not null,
    entry_type text not null check (entry_type IN ('DEBIT','CREDIT')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE Idempotency_record (
    idempotency_key TEXT PRIMARY KEY,
    transaction_id TEXT NOT NULL REFERENCES transfer(transaction_id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);