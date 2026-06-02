package com.walletservice.domain.entity;

import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * IdempotencyRecord entity tracking idempotency keys for duplicate request detection.
 *
 * Design notes:
 * - idempotencyKey is globally unique (primary key)
 * - Each key maps to exactly one transfer (exactly-once semantics)
 * - request_hash allows detecting if the same idempotency key is used with different request bodies
 * - Entries are immutable and created only once per idempotency key
 */
@Entity
@Table(name = "idempotency_records")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class IdempotencyRecord {
    @Id
    @Column(name = "idempotency_key")
    private String idempotencyKey;

    @Column(name = "transfer_id", nullable = false)
    private String transferId;

    /**
     * Hash of the request body to detect if the same idempotency key is reused
     * with different request parameters (which is an error).
     */
    @Column(name = "request_hash", nullable = false)
    private String requestHash;

    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;

    @PrePersist
    protected void onCreate() {
        this.createdAt = LocalDateTime.now();
    }
}

