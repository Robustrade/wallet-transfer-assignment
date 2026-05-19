-- +goose Up
CREATE TABLE wallets (
    id         TEXT          PRIMARY KEY,
    name       TEXT          NOT NULL,
    balance    NUMERIC(18,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE wallets;
