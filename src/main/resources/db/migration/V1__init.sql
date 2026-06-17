CREATE TABLE wallets (
id UUID PRIMARY KEY,
balance NUMERIC(19,2) NOT NULL,
version BIGINT
);

CREATE TABLE transfers (
id UUID PRIMARY KEY,
source_wallet_id UUID NOT NULL,
destination_wallet_id UUID NOT NULL,
amount NUMERIC(19,2) NOT NULL,
status VARCHAR(50) NOT NULL,
idempotency_key VARCHAR(255) NOT NULL UNIQUE,
created_at TIMESTAMP,


CONSTRAINT fk_transfer_source_wallet
    FOREIGN KEY (source_wallet_id)
    REFERENCES wallets(id),

CONSTRAINT fk_transfer_destination_wallet
    FOREIGN KEY (destination_wallet_id)
    REFERENCES wallets(id)


);

CREATE TABLE ledger_entries (
id UUID PRIMARY KEY,
transfer_id UUID NOT NULL,
wallet_id UUID NOT NULL,
amount NUMERIC(19,2) NOT NULL,
entry_type VARCHAR(20) NOT NULL,
created_at TIMESTAMP NOT NULL,


CONSTRAINT fk_ledger_transfer
    FOREIGN KEY (transfer_id)
    REFERENCES transfers(id),

CONSTRAINT fk_ledger_wallet
    FOREIGN KEY (wallet_id)
    REFERENCES wallets(id),

CONSTRAINT chk_entry_type
    CHECK (entry_type IN ('DEBIT', 'CREDIT'))


);

CREATE INDEX idx_transfer_source_wallet
ON transfers(source_wallet_id);

CREATE INDEX idx_transfer_destination_wallet
ON transfers(destination_wallet_id);

CREATE INDEX idx_ledger_transfer
ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_wallet
ON ledger_entries(wallet_id);
