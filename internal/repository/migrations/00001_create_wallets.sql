-- +goose Up
CREATE TABLE IF NOT EXISTS wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS wallets;
