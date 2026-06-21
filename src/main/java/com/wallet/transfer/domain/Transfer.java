package com.wallet.transfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "transfers")
public class Transfer {

    @Id
    private String id;

    @Column(name = "idempotency_key", nullable = false, unique = true, updatable = false)
    private String idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false, updatable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, updatable = false)
    private String toWalletId;

    @Column(nullable = false, updatable = false)
    private Long amount;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private TransferStatus status;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    @Column(name = "failure_balance")
    private Long failureBalance;

    protected Transfer() {
    }

    public Transfer(String idempotencyKey, String fromWalletId, String toWalletId, Long amount) {
        this.id = UUID.randomUUID().toString();
        this.idempotencyKey = idempotencyKey;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = TransferStatus.PENDING;
    }

    @PrePersist
    void onCreate() {
        this.createdAt = Instant.now();
        this.updatedAt = Instant.now();
    }

    @PreUpdate
    void onUpdate() {
        this.updatedAt = Instant.now();
    }

    public String getId() {
        return id;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }

    public String getFromWalletId() {
        return fromWalletId;
    }

    public String getToWalletId() {
        return toWalletId;
    }

    public Long getAmount() {
        return amount;
    }

    public TransferStatus getStatus() {
        return status;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }

    public void markProcessed() {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                "Cannot transition from " + this.status + " to PROCESSED");
        }
        this.status = TransferStatus.PROCESSED;
    }

    public void markFailed(Long availableBalance) {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                "Cannot transition from " + this.status + " to FAILED");
        }
        this.status = TransferStatus.FAILED;
        this.failureBalance = availableBalance;
    }

    public Long getFailureBalance() {
        return failureBalance;
    }
}
