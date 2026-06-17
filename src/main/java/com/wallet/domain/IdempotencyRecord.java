package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

public record IdempotencyRecord(
        String idempotencyKey,
        UUID transferId,
        String requestHash,
        String responseBody,
        int httpStatus,
        Instant createdAt) {}
