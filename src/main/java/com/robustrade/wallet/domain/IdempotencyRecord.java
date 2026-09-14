package com.robustrade.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;
import lombok.AccessLevel;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Getter;
import lombok.NoArgsConstructor;
import lombok.Setter;

/**
 * Idempotency key → transfer mapping. Primary key is {@code idempotency_key} (Flyway V1).
 * Schema owned by Flyway; Hibernate {@code ddl-auto=none}.
 */
@Entity
@Table(name = "idempotency_records")
@Getter
@Setter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@AllArgsConstructor
@Builder
public class IdempotencyRecord {

    @Id
    @Column(name = "idempotency_key", nullable = false, length = 128)
    private String idempotencyKey;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Column(name = "request_hash", nullable = false, length = 128)
    private String requestHash;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    public static IdempotencyRecord create(String idempotencyKey, UUID transferId, String requestHash) {
        return IdempotencyRecord.builder()
                .idempotencyKey(idempotencyKey)
                .transferId(transferId)
                .requestHash(requestHash)
                .createdAt(Instant.now())
                .build();
    }
}
