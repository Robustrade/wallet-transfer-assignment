INSERT INTO wallets (id, opening_balance, balance) VALUES
    ('wallet_1', 1000, 1000),
    ('wallet_2', 1000, 1000),
    ('wallet_3', 0, 0)
ON CONFLICT (id) DO NOTHING;
