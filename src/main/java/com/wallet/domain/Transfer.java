package com.wallet.domain;

import com.wallet.exception.IllegalStateTransitionException;
import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.PrePersist;
import jakarta.persistence.PreUpdate;
import jakarta.persistence.Table;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

@Entity
@Table(name = "transfers")
public class Transfer {

    @Id
    @Column(name = "id")
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, unique = true)
    private String idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false)
    private String toWalletId;

    @Column(name = "amount", nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 16)
    private TransferStatus status;

    @Column(name = "failure_reason", length = 512)
    private String failureReason;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    protected Transfer() {

    }

    public static Transfer createPending(String idempotencyKey, String fromWalletId, String toWalletId, BigDecimal amount) {
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must differ");
        }
        if (amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        Transfer transfer = new Transfer();
        transfer.id = UUID.randomUUID();
        transfer.idempotencyKey = idempotencyKey;
        transfer.fromWalletId = fromWalletId;
        transfer.toWalletId = toWalletId;
        transfer.amount = amount;
        transfer.status = TransferStatus.PENDING;
        return transfer;
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

    /** Applies a guarded transition; throws if the transition is not legal from the current state. */
    public void markProcessed() {
        transitionTo(TransferStatus.PROCESSED, null);
    }

    public void markFailed(String reason) {
        transitionTo(TransferStatus.FAILED, reason);
    }

    private void transitionTo(TransferStatus next, String reason) {
        if (!this.status.canTransitionTo(next)) {
            throw new IllegalStateTransitionException(this.id, this.status, next);
        }
        this.status = next;
        this.failureReason = reason;
    }

    public UUID getId() {
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

    public BigDecimal getAmount() {
        return amount;
    }

    public TransferStatus getStatus() {
        return status;
    }

    public String getFailureReason() {
        return failureReason;
    }

    public Instant getCreatedAt() {
        return createdAt;
    }

    public Instant getUpdatedAt() {
        return updatedAt;
    }
}