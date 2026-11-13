package com.wallettransfer.domain;

import java.time.Instant;

/** Links an idempotency key to the transfer it produced and to a fingerprint of the request. */
public record IdempotencyRecord(
    String key, String requestHash, String transferId, Instant createdAt) {}
