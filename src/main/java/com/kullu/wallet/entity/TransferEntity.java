package com.kullu.wallet.entity;

import java.time.Instant;
import java.util.UUID;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;

import org.hibernate.annotations.CreationTimestamp;
import org.hibernate.annotations.UpdateTimestamp;

import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
@Entity
@Table(name = "transfers")
public class TransferEntity {

    @Id
    @Column(name = "id", nullable = false, updatable = false)
    private UUID id;

    @Column(name = "from_wallet_id", nullable = false, updatable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, updatable = false)
    private String toWalletId;

    @Column(name = "amount", nullable = false, updatable = false)
    private long amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 16)
    private TransferStatus status;

    @Column(name = "failure_reason", length = 64)
    private String failureReason;

    @CreationTimestamp
    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @UpdateTimestamp
    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public TransferEntity(final UUID id, final String fromWalletId, final String toWalletId, final long amount) {
        if (id == null || fromWalletId == null || toWalletId == null) {
            throw new IllegalArgumentException("ids must not be null");
        }
        if (fromWalletId.isBlank() || toWalletId.isBlank()) {
            throw new IllegalArgumentException("wallet ids must not be blank");
        }
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("from and to wallets must differ");
        }
        if (amount <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        this.id = id;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = TransferStatus.PENDING;
    }

    public void markProcessed() {
        requireFrom(TransferStatus.PENDING);
        this.status = TransferStatus.PROCESSED;
    }

    public void markFailed(final String reason) {
        if (reason == null || reason.isBlank()) {
            throw new IllegalArgumentException("failure reason must not be blank");
        }
        requireFrom(TransferStatus.PENDING);
        this.status = TransferStatus.FAILED;
        this.failureReason = reason;
    }

    private void requireFrom(final TransferStatus expected) {
        if (this.status != expected) {
            throw new IllegalStateException(
                "illegal transition from " + this.status + " (expected " + expected + ")");
        }
    }
}
