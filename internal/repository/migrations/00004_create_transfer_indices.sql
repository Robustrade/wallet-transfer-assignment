-- +goose Up
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet_id ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet_id ON transfers(to_wallet_id);

-- +goose Down
DROP INDEX IF EXISTS idx_transfers_to_wallet_id;
DROP INDEX IF EXISTS idx_transfers_from_wallet_id;

