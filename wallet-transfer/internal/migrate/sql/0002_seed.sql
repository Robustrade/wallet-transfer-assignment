-- Seed data: a few demo wallets so transfers can be performed immediately
-- after running migrations. Idempotent, so re-running is safe.
-- Balances are in minor units (cents): 100000 = 1000.00.

INSERT INTO wallets (id, balance, currency) VALUES
  ('wallet_1', 100000, 'USD'),
  ('wallet_2',  50000, 'USD'),
  ('wallet_3',  25000, 'USD'),
  ('wallet_4',      0, 'USD')
ON CONFLICT (id) DO NOTHING;
