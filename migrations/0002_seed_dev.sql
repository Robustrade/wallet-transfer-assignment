INSERT INTO wallets (id, currency, balance) VALUES
    ('wallet_1', 'INR', 100000),
    ('wallet_2', 'INR',  50000),
    ('wallet_3', 'INR',      0),
    ('wallet_usd', 'USD', 25000)
ON CONFLICT (id) DO NOTHING;
