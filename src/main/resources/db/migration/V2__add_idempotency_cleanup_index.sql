-- V2__add_idempotency_cleanup_index.sql
-- Backs the periodic `DELETE ... WHERE first_seen_at < threshold` cleanup.
-- B-tree (default) — handles the range predicate efficiently and avoids the
-- full-table scan that would otherwise block writes on a large table.

CREATE INDEX idx_idempotency_first_seen_at ON idempotency_records(first_seen_at);
