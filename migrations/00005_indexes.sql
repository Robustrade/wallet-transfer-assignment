-- +goose Up
-- Postgres does not auto-index FK columns. These speed up per-wallet lookups
-- and FK integrity checks. (idempotency_key already has an index from its
-- UNIQUE constraint; ledger_entries.transfer_id is the leftmost column of
-- UNIQUE(transfer_id, type) so it is already indexed for reconciliation.)
CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id   ON transfers (to_wallet_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);

-- Partial index for the stale-PENDING reconciliation sweep.
CREATE INDEX idx_transfers_pending ON transfers (created_at) WHERE status = 'PENDING';

-- +goose Down
DROP INDEX idx_transfers_pending;
DROP INDEX idx_ledger_entries_wallet_id;
DROP INDEX idx_transfers_to_wallet_id;
DROP INDEX idx_transfers_from_wallet_id;
