package com.api.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;
import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Entity
@Table(name = "idempotency_records")
@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
public class IdempotencyRecord {

    @Id
    @Column(name = "idempotency_key", nullable = false, updatable = false)
    private String idempotencyKey;

    @Column(name = "transfer_id", updatable = false)
    private UUID transferId;

    @Column(name = "response_status", nullable = false, updatable = false)
    private Integer responseStatus;

    @Column(name = "response_body", nullable = false, updatable = false)
    private String responseBody;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    public static IdempotencyRecord of(
            String idempotencyKey, UUID transferId, int responseStatus, String responseBody) {
        IdempotencyRecord record = new IdempotencyRecord();
        record.idempotencyKey = idempotencyKey;
        record.transferId = transferId;
        record.responseStatus = responseStatus;
        record.responseBody = responseBody;
        record.createdAt = Instant.now();
        return record;
    }
}
