package com.wallettransfer.idempotency.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.Id;
import jakarta.persistence.Lob;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "idempotency_records")
public class IdempotencyRecord {

    @Id
    @Column(name = "idempotency_key", length = 128)
    private String idempotencyKey;

    @Column(name = "transfer_id", nullable = false)
    private UUID transferId;

    @Lob
    @Column(name = "response_body")
    private String responseBody;

    @Column(name = "http_status", nullable = false)
    private int httpStatus;

    @Column(nullable = false)
    private Instant createdAt = Instant.now();

    protected IdempotencyRecord() {
    }

    public IdempotencyRecord(String idempotencyKey, UUID transferId) {
        this.idempotencyKey = idempotencyKey;
        this.transferId = transferId;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public String getResponseBody() {
        return responseBody;
    }

    public int getHttpStatus() {
        return httpStatus;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public void setResponse(String responseBody, int httpStatus) {
        this.responseBody = responseBody;
        this.httpStatus = httpStatus;
    }
}
