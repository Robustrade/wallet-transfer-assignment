DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_transfers_from_wallet'
    ) THEN
        ALTER TABLE transfers
            ADD CONSTRAINT fk_transfers_from_wallet
            FOREIGN KEY (from_wallet_id)
            REFERENCES wallets (wallet_id);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_transfers_to_wallet'
    ) THEN
        ALTER TABLE transfers
            ADD CONSTRAINT fk_transfers_to_wallet
            FOREIGN KEY (to_wallet_id)
            REFERENCES wallets (wallet_id);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_ledger_entries_wallet'
    ) THEN
        ALTER TABLE ledger_entries
            ADD CONSTRAINT fk_ledger_entries_wallet
            FOREIGN KEY (wallet_id)
            REFERENCES wallets (wallet_id);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_ledger_entries_transfer'
    ) THEN
        ALTER TABLE ledger_entries
            ADD CONSTRAINT fk_ledger_entries_transfer
            FOREIGN KEY (transfer_id)
            REFERENCES transfers (transfer_id);
    END IF;
END $$;