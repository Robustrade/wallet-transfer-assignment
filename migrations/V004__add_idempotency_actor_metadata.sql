-- =====================================================================
-- V004: Add actor metadata to idempotency_records
-- =====================================================================
-- Adds columns to store who initiated the request and basic caller metadata
-- Useful for audit, debugging, and enforcing replay policies

ALTER TABLE idempotency_records
  ADD COLUMN created_by TEXT NULL,
  ADD COLUMN caller_ip TEXT NULL,
  ADD COLUMN user_agent TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_idempotency_created_by
  ON idempotency_records(created_by);

CREATE INDEX IF NOT EXISTS idx_idempotency_caller_ip
  ON idempotency_records(caller_ip);

-- No NOT NULL constraints to allow gradual rollout and backward compatibility
