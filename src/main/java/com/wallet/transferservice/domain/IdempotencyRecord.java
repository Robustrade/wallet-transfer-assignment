package com.wallet.transferservice.domain;

import jakarta.persistence.*;
import org.springframework.data.domain.Persistable;

import java.math.BigDecimal;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.UUID;

@Entity
@Table(name = "idempotency_records")
public class IdempotencyRecord implements Persistable<UUID> {

    @Id
    @Column(name = "idempotency_key")
    private UUID idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false)
    private UUID fromWalletId;

    @Column(name = "to_wallet_id", nullable = false)
    private UUID toWalletId;

    @Column(name = "amount", nullable = false, precision = 18, scale = 2)
    private BigDecimal amount;

    @Column(name = "transfer_id")
    private UUID transferId;

    @Column(name = "http_status_code")
    private Short httpStatusCode;

    @Column(name = "response_body", columnDefinition = "text")
    private String responseBody;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @Column(name = "expires_at", nullable = false)
    private Instant expiresAt;

    @Transient
    private boolean isNew = true;

    @PostPersist
    @PostLoad
    void markNotNew() {
        this.isNew = false;
    }

    // ── Persistable ───────────────────────────────────────────

    @Override
    public UUID getId() {
        return idempotencyKey;
    }

    @Override
    public boolean isNew() {
        return isNew;
    }

    // ── Constructors ──────────────────────────────────────────

    public IdempotencyRecord() {
    }

    public IdempotencyRecord(UUID idempotencyKey, UUID fromWalletId, UUID toWalletId, BigDecimal amount) {
        this.idempotencyKey = idempotencyKey;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.isNew = true;
        Instant now = Instant.now();
        this.createdAt = now;
        this.expiresAt = now.plus(24, ChronoUnit.HOURS);
    }

    // ── Getters & Setters ─────────────────────────────────────

    public UUID getIdempotencyKey() {
        return idempotencyKey;
    }

    public void setIdempotencyKey(UUID idempotencyKey) {
        this.idempotencyKey = idempotencyKey;
    }

    public UUID getFromWalletId() {
        return fromWalletId;
    }

    public void setFromWalletId(UUID fromWalletId) {
        this.fromWalletId = fromWalletId;
    }

    public UUID getToWalletId() {
        return toWalletId;
    }

    public void setToWalletId(UUID toWalletId) {
        this.toWalletId = toWalletId;
    }

    public BigDecimal getAmount() {
        return amount;
    }

    public void setAmount(BigDecimal amount) {
        this.amount = amount;
    }

    public UUID getTransferId() {
        return transferId;
    }

    public void setTransferId(UUID transferId) {
        this.transferId = transferId;
    }

    public Short getHttpStatusCode() {
        return httpStatusCode;
    }

    public void setHttpStatusCode(Short httpStatusCode) {
        this.httpStatusCode = httpStatusCode;
    }

    public String getResponseBody() {
        return responseBody;
    }

    public void setResponseBody(String responseBody) {
        this.responseBody = responseBody;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getExpiresAt() {
        return expiresAt;
    }

    public void setExpiresAt(Instant expiresAt) {
        this.expiresAt = expiresAt;
    }

    /**
     * Returns true if this record has a completed response stored
     * (i.e., the original request finished processing).
     */
    public boolean isCompleted() {
        return this.responseBody != null && this.httpStatusCode != null;
    }

    /**
     * Checks if the given request parameters match the ones stored in this record.
     */
    public boolean parametersMatch(UUID fromWalletId, UUID toWalletId, BigDecimal amount) {
        return this.fromWalletId.equals(fromWalletId)
                && this.toWalletId.equals(toWalletId)
                && this.amount.compareTo(amount) == 0;
    }
}

