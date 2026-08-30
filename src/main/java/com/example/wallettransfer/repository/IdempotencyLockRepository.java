package com.example.wallettransfer.repository;

import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class IdempotencyLockRepository {

    private final JdbcTemplate jdbcTemplate;

    public IdempotencyLockRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    /**
     * Acquires a PostgreSQL transaction-scoped advisory lock for the given idempotency key.
     *
     * Why this exists:
     * Without this lock, two concurrent requests carrying the same idempotency key could
     * both check the DB, both find no existing record, and both proceed to create a transfer —
     * effectively charging the source wallet twice. This is the classic "check-then-act" race.
     *
     * How it works:
     * pg_advisory_xact_lock() is a transaction-scoped mutex inside PostgreSQL. Only one transaction
     * can hold the lock for a given key at a time — any other transaction trying to acquire
     * the same lock will block until the first one commits or rolls back.
     * The lock is automatically released at the end of the transaction, so no cleanup needed.
     *
     * The key is hashed to a bigint (hashtextextended) because pg_advisory_xact_lock takes
     * a numeric slot, not a string. A theoretical hash collision between two different keys
     * just means extra waiting — not incorrect behavior, since the actual duplicate check
     * still uses the full string key in the idempotency_records table.
     *
     * Different idempotency keys hash to different slots, so unrelated requests
     * are never blocked by each other — only same-key requests are serialized.
     */
    public void lock(String idempotencyKey) {
        jdbcTemplate.queryForList(
                "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))",
                idempotencyKey
        );
    }
}
