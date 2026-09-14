-- Allow ledger rows without a transfer for wallet opening-balance funding.
ALTER TABLE ledger_entries ALTER COLUMN transfer_id DROP NOT NULL;
