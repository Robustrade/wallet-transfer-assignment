-- Demo wallets with opening balances (minor units). Safe to re-run: existing wallets,
-- and therefore balances changed by real transfers, are never overwritten.
INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 10000),
    ('wallet_2', 5000),
    ('wallet_3', 0)
ON CONFLICT (id) DO NOTHING;
