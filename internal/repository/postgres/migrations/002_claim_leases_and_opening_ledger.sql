ALTER TABLE idempotency_records ADD COLUMN owner_token TEXT;
ALTER TABLE idempotency_records ADD COLUMN lease_expires_at TIMESTAMPTZ;

UPDATE idempotency_records
SET owner_token = COALESCE(owner_token, 'legacy-owner'),
    lease_expires_at = COALESCE(lease_expires_at, NOW() - INTERVAL '1 second')
WHERE state = 'IN_PROGRESS';

ALTER TABLE idempotency_records
ADD CONSTRAINT idempotency_in_progress_lease_check
CHECK (state <> 'IN_PROGRESS' OR (owner_token IS NOT NULL AND lease_expires_at IS NOT NULL));

ALTER TABLE ledger_entries ALTER COLUMN wallet_id DROP NOT NULL;
ALTER TABLE ledger_entries ALTER COLUMN transfer_id DROP NOT NULL;
ALTER TABLE ledger_entries ADD COLUMN account_id TEXT;
ALTER TABLE ledger_entries ADD COLUMN operation_id TEXT;

UPDATE ledger_entries
SET account_id = wallet_id,
    operation_id = transfer_id;

ALTER TABLE ledger_entries ALTER COLUMN account_id SET NOT NULL;
ALTER TABLE ledger_entries ALTER COLUMN operation_id SET NOT NULL;
ALTER TABLE ledger_entries ADD CONSTRAINT ledger_entries_operation_type_key UNIQUE (operation_id, entry_type);
