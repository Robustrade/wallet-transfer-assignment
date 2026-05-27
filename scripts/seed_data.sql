-- Seed data for testing
-- This file creates test wallets with initial balances

-- Clean up existing test data (optional - comment out if you want to preserve data)
-- DELETE FROM ledger_entries WHERE wallet_id IN ('wallet_1', 'wallet_2', 'wallet_999');
-- DELETE FROM transfers WHERE from_wallet_id IN ('wallet_1', 'wallet_2', 'wallet_999') OR to_wallet_id IN ('wallet_1', 'wallet_2', 'wallet_999');
-- DELETE FROM idempotency_records;
-- DELETE FROM wallets WHERE id IN ('wallet_1', 'wallet_2', 'wallet_999');

-- Insert test wallets
INSERT INTO wallets (id, balance, created_at, updated_at)
VALUES
  ('wallet_1', 1000.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('wallet_2', 500.00, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE
  SET balance = EXCLUDED.balance,
      updated_at = CURRENT_TIMESTAMP;

-- Verify data
SELECT 'Test wallets created:' as status;
SELECT id, balance FROM wallets WHERE id IN ('wallet_1', 'wallet_2');
