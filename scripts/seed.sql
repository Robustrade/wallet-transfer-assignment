-- Seed sample wallets for manual testing
CREATE EXTENSION IF NOT EXISTS pgcrypto;

INSERT INTO wallets (id, balance) VALUES
  (gen_random_uuid()::text, 100000),
  (gen_random_uuid()::text, 50000),
  (gen_random_uuid()::text, 25000);
