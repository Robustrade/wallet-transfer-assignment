-- R__seed_wallets.sql
-- Seed test wallets with initial balances (amounts in cents)

INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000000) ON CONFLICT (id) DO NOTHING;
INSERT INTO wallets (id, balance) VALUES ('wallet_2', 1000000) ON CONFLICT (id) DO NOTHING;
INSERT INTO wallets (id, balance) VALUES ('wallet_3', 500000) ON CONFLICT (id) DO NOTHING;
INSERT INTO wallets (id, balance) VALUES ('wallet_4', 0) ON CONFLICT (id) DO NOTHING;
