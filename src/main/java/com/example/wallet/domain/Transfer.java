package com.example.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Index;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

@Entity
@Table(
        name = "transfers",
        indexes = @Index(name = "idx_transfers_idempotency_key", columnList = "idempotency_key", unique = true))
public class Transfer {

    @Id
    private String id;

    @Column(name = "from_wallet_id", nullable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false)
    private String toWalletId;

    @Column(nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private TransferStatus status;

    @Column(name = "error_message")
    private String errorMessage;

    @Column(name = "idempotency_key", nullable = false, unique = true)
    private String idempotencyKey;

    @CreationTimestamp
    @Column(nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(nullable = false)
    private Instant updatedAt;

    protected Transfer() {
    }

    public Transfer(
            String id,
            String fromWalletId,
            String toWalletId,
            BigDecimal amount,
            String idempotencyKey) {
        this.id = id;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.idempotencyKey = idempotencyKey;
        this.status = TransferStatus.PENDING;
    }

    public void markProcessed() {
        status = TransferStatus.PROCESSED;
        errorMessage = null;
    }

    public void markFailed(String message) {
        status = TransferStatus.FAILED;
        errorMessage = message;
    }

    public String getId() {
        return id;
    }

    public String getFromWalletId() {
        return fromWalletId;
    }

    public String getToWalletId() {
        return toWalletId;
    }

    public BigDecimal getAmount() {
        return amount;
    }

    public TransferStatus getStatus() {
        return status;
    }

    public String getErrorMessage() {
        return errorMessage;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }
}
