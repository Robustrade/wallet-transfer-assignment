package com.wallettransfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;

/**
 * Durable idempotency record: maps a client key to a request hash and optional transfer id.
 * Prevents duplicate transfers when the same idempotency key is retried.
 */
@Entity
@Table(name = "idempotency_records")
public class IdempotencyRecord {

    @Id
    @Column(name = "idempotency_key")
    private String idempotencyKey;

    /** SHA-256 fingerprint of from|to|amount. */
    @Column(name = "request_hash", nullable = false)
    private String requestHash;

    /** Set once the transfer row is created. */
    @Column(name = "transfer_id")
    private String transferId;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    /** Required by JPA. */
    protected IdempotencyRecord() {}

    /**
     * Creates a new idempotency reservation (transfer id may be null until linked).
     */
    public IdempotencyRecord(String idempotencyKey, String requestHash, String transferId, Instant createdAt) {
        this.idempotencyKey = idempotencyKey;
        this.requestHash = requestHash;
        this.transferId = transferId;
        this.createdAt = createdAt;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public String getRequestHash() {
        return requestHash;
    }

    public String getTransferId() {
        return transferId;
    }

    public void setTransferId(String transferId) {
        this.transferId = transferId;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }
}
