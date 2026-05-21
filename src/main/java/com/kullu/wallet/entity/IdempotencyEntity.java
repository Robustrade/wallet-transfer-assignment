package com.kullu.wallet.entity;

import java.time.Instant;
import java.util.UUID;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;

import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

/**
 * Durable record of an idempotent request.
 */
@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@Entity
@Table(name = "idempotency_records")
public class IdempotencyEntity {

    @Id
    @Column(name = "key", nullable = false, updatable = false, length = 64)
    private String key;

    @Column(name = "request_hash", nullable = false, updatable = false, length = 64)
    private String requestHash;

    @Column(name = "transfer_id", nullable = false, updatable = false)
    private UUID transferId;

    @Column(name = "response_status")
    private Short responseStatus;

    @Column(name = "response_body", columnDefinition = "text")
    private String responseBody;

    @CreationTimestamp
    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public IdempotencyEntity(final String key, final String requestHash, final UUID transferId) {
        if (key == null || key.isBlank()) {
            throw new IllegalArgumentException("key must not be blank");
        }
        if (requestHash == null || requestHash.length() != 64) {
            throw new IllegalArgumentException("requestHash must be 64 hex chars");
        }
        if (transferId == null) {
            throw new IllegalArgumentException("transferId must not be null");
        }
        this.key = key;
        this.requestHash = requestHash;
        this.transferId = transferId;
    }

    public void storeResponse(final short status, final String body) {
        if (body == null) {
            throw new IllegalArgumentException("response body must not be null");
        }
        this.responseStatus = status;
        this.responseBody = body;
    }

    public boolean hasResponse() {
        return responseStatus != null && responseBody != null;
    }
}
