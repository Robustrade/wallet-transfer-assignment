
CREATE TABLE wallets (
	wallet_id UUID PRIMARY KEY,
	balance NUMERIC NOT NULL CHECK (balance >= 0),
	created_at TIMESTAMP DEFAULT NOW(),
	updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE transfers (
	transfer_id UUID PRIMARY KEY,
	idempotency_key VARCHAR(255) UNIQUE NOT NULL,
	from_wallet_id UUID REFERENCES wallets(wallet_id),
	to_wallet_id UUID REFERENCES wallets(wallet_id),
	amount NUMERIC NOT NULL CHECK (amount > 0),
	state VARCHAR(20) NOT NULL CHECK (state IN ('PENDING','PROCESSED','FAILED')),
	created_at TIMESTAMP DEFAULT NOW(),
	updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE ledger_entries (
	entry_id UUID PRIMARY KEY,
	wallet_id UUID REFERENCES wallets(wallet_id),
	transfer_id UUID REFERENCES transfers(transfer_id),
	type VARCHAR(10) NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
	amount NUMERIC NOT NULL,
	created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE idempotency_records (
	idempotency_key VARCHAR(255) PRIMARY KEY,
	transfer_id UUID REFERENCES transfers(transfer_id) ON DELETE CASCADE,
	response_payload JSONB,
	created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for performance
CREATE INDEX idx_transfers_idempotency_key ON transfers(idempotency_key);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_idempotency_records_created_at ON idempotency_records(created_at);

-- Constraints for data integrity
ALTER TABLE transfers DROP CONSTRAINT IF EXISTS fk_from_wallet;
ALTER TABLE transfers ADD CONSTRAINT fk_from_wallet 
	FOREIGN KEY (from_wallet_id) REFERENCES wallets(wallet_id) ON DELETE RESTRICT;

ALTER TABLE transfers DROP CONSTRAINT IF EXISTS fk_to_wallet;
ALTER TABLE transfers ADD CONSTRAINT fk_to_wallet 
	FOREIGN KEY (to_wallet_id) REFERENCES wallets(wallet_id) ON DELETE RESTRICT;

ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS fk_ledger_wallet;
ALTER TABLE ledger_entries ADD CONSTRAINT fk_ledger_wallet 
	FOREIGN KEY (wallet_id) REFERENCES wallets(wallet_id) ON DELETE RESTRICT;

ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS fk_ledger_transfer;
ALTER TABLE ledger_entries ADD CONSTRAINT fk_ledger_transfer 
	FOREIGN KEY (transfer_id) REFERENCES transfers(transfer_id) ON DELETE CASCADE;
