INSERT INTO wallets (id, balance)
VALUES
    ('wallet_1', 1000),
    ('wallet_2', 500)
ON CONFLICT (id) DO NOTHING;