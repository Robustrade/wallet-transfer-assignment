-- Local/dev/test convenience seed. Not part of migrations: this is sample
-- data, not schema. Wallet creation is out of scope for the assignment's
-- API surface (see DESIGN.md), so wallets are seeded directly for trying
-- the service out locally or in tests.
INSERT INTO wallets (id, balance) VALUES
  ('wallet_1', 100000),
  ('wallet_2', 50000),
  ('wallet_3', 0)
ON CONFLICT (id) DO NOTHING;
