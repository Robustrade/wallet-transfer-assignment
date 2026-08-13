INSERT INTO wallets (id, balance, currency)
VALUES
    ('wallet_1', 50000, 'USD'),
    ('wallet_2', 20000, 'USD'),
    ('wallet_3', 0, 'USD')
ON CONFLICT (id) DO NOTHING;
