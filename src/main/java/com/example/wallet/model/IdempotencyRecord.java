package com.example.wallet.model;

import java.time.Instant;

/**
 * Represents a stored idempotency record for any API operation.
 *
 * Stores the full serialized response body and HTTP status so that
 * duplicate requests can be replied to exactly — without re-executing
 * any side effects.
 *
 * The composite key (idempotencyKey + endpoint) allows this table to
 * serve multiple API endpoints, not just transfers.
 *
 * Records expire at expiresAt — expired records are ignored on lookup
 * and are eligible for cleanup by a scheduled job.
 */
public record IdempotencyRecord(
        String idempotencyKey,
        String endpoint,
        String responseBody,
        int httpStatus,
        Instant createdAt,
        Instant expiresAt
) {}

