package com.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;

import java.time.Instant;
import java.util.UUID;


@Entity
@Table(name = "idempotency_records")
public class IdempotencyRecord {

    @Id
    @Column(name = "idempotency_key")
    private String idempotencyKey;

    @Column(name = "request_hash", nullable = false, length = 128)
    private String requestHash;

    @Column(name = "transfer_id")
    private UUID transferId;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    protected IdempotencyRecord() {
        // JPA
    }

    public IdempotencyRecord(String idempotencyKey, String requestHash, UUID transferId) {
        this.idempotencyKey = idempotencyKey;
        this.requestHash = requestHash;
        this.transferId = transferId;
    }

    public void attachTransfer(UUID transferId) {
        this.transferId = transferId;
    }

    @PrePersist
    protected void onCreate() {
        Instant now = Instant.now();
        this.createdAt = now;
        this.updatedAt = now;
    }

    @PreUpdate
    protected void onUpdate() {
        this.updatedAt = Instant.now();
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

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }
}