package com.example.wallet.repository;

import com.example.wallet.model.IdempotencyRecord;

import java.time.Instant;
import java.util.Optional;

public interface IdempotencyRepository {

    /**
     * Look up a non-expired idempotency record by key and endpoint.
     * Returns empty if the record does not exist or has expired.
     */
    Optional<IdempotencyRecord> find(String idempotencyKey, String endpoint);

    /**
     * Persist an idempotency record atomically with the calling transaction.
     * Uses ON CONFLICT DO NOTHING — if two concurrent requests race to save
     * the same key, the second write is silently ignored.
     */
    void save(String idempotencyKey, String endpoint, String responseBody, int httpStatus, Instant expiresAt);

    /**
     * Delete all expired records.
     * Intended to be called by a scheduled cleanup job.
     *
     * @return number of records deleted
     */
    int deleteExpired();
}

