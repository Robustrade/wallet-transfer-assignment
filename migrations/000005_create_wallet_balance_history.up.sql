CREATE TABLE wallet_balance_history (
    id BIGSERIAL PRIMARY KEY,
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    balance BIGINT NOT NULL CHECK (balance >= 0),
    change BIGINT NOT NULL,
    reason VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX wallet_balance_history_wallet_idx ON wallet_balance_history(wallet_id, created_at DESC, id DESC);
