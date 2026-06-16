package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

/**
 * Read-side projection of a persisted idempotency row. State changes are
 * driven by targeted UPDATE / INSERT statements in
 * {@link com.wallet.repository.IdempotencyRecordRepository}; this class
 * carries no mutators.
 */
public class IdempotencyRecord {

    private final String clientId;
    private final String idempotencyKey;
    private final String requestHash;
    private final UUID transferId;
    private final IdempotencyStatus status;
    private final Integer responseStatus;
    private final String responseBody;
    private final Integer timesSeen;
    private final Instant firstSeenAt;
    private final Instant lastSeenAt;

    public IdempotencyRecord(String clientId, String idempotencyKey, String requestHash, UUID transferId,
                             IdempotencyStatus status, Integer responseStatus, String responseBody,
                             Integer timesSeen, Instant firstSeenAt, Instant lastSeenAt) {
        this.clientId = clientId;
        this.idempotencyKey = idempotencyKey;
        this.requestHash = requestHash;
        this.transferId = transferId;
        this.status = status;
        this.responseStatus = responseStatus;
        this.responseBody = responseBody;
        this.timesSeen = timesSeen;
        this.firstSeenAt = firstSeenAt;
        this.lastSeenAt = lastSeenAt;
    }

    public String getClientId() {
        return clientId;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public String getRequestHash() {
        return requestHash;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public IdempotencyStatus getStatus() {
        return status;
    }

    public Integer getResponseStatus() {
        return responseStatus;
    }

    public String getResponseBody() {
        return responseBody;
    }

    public Integer getTimesSeen() {
        return timesSeen;
    }

    public Instant getFirstSeenAt() {
        return firstSeenAt;
    }

    public Instant getLastSeenAt() {
        return lastSeenAt;
    }
}
