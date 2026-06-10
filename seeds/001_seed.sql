-- Seed data: test wallets with initial balances
-- This file is separate from schema DDL so it can be skipped in production.

INSERT INTO wallets (id, balance, currency) VALUES
    ('wallet_1', 10000, 'USD'),
    ('wallet_2', 5000, 'USD'),
    ('wallet_3', 0, 'USD')
ON CONFLICT (id) DO NOTHING;
