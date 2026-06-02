package com.walletservice.domain.entity;

import jakarta.persistence.*;
import lombok.*;

import java.time.LocalDateTime;
import java.util.UUID;

/**
 * Transfer entity representing a wallet-to-wallet transfer.
 *
 * Design notes:
 * - Uses optimistic locking to detect concurrent state modifications
 * - Status is an enum enforcing valid state transitions
 * - Transfers are immutable once created; only status can change
 * - Deletions are not supported; failed transfers remain in FAILED state
 */
@Entity
@Table(name = "transfers")
@Getter
@Setter
@NoArgsConstructor
@AllArgsConstructor
@Builder
public class Transfer {
    @Id
    private String id;

    @Column(name = "from_wallet_id", nullable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false)
    private String toWalletId;

    @Column(nullable = false)
    private Long amount;

    @Enumerated(EnumType.STRING)
    @Column(nullable = false)
    private TransferStatus status;

    /**
     * Optimistic locking version to detect concurrent modifications.
     * Ensures safe state transitions under concurrent writes.
     */
    @Version
    @Column(nullable = false)
    private Long version;

    @Column(nullable = false, updatable = false)
    private LocalDateTime createdAt;

    @Column(nullable = false)
    private LocalDateTime updatedAt;

    @PrePersist
    protected void onCreate() {
        this.id = UUID.randomUUID().toString();
        this.createdAt = LocalDateTime.now();
        this.updatedAt = LocalDateTime.now();
        if (this.status == null) {
            this.status = TransferStatus.PENDING;
        }
    }

    @PreUpdate
    protected void onUpdate() {
        this.updatedAt = LocalDateTime.now();
    }

    /**
     * Validates that this transfer can be marked as processed.
     * Only PENDING transfers can move to PROCESSED.
     */
    public void validateCanProcess() {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                String.format("Transfer must be in PENDING state to process. Current state: %s", this.status));
        }
    }

    /**
     * Validates that this transfer can be marked as failed.
     * Only PENDING transfers can move to FAILED.
     */
    public void validateCanFail() {
        if (this.status != TransferStatus.PENDING) {
            throw new IllegalStateException(
                String.format("Transfer must be in PENDING state to fail. Current state: %s", this.status));
        }
    }

    /**
     * Validates that transfer is valid before processing.
     */
    public void validate() {
        if (this.fromWalletId == null || this.toWalletId == null) {
            throw new IllegalArgumentException("Source and destination wallet IDs are required");
        }
        if (this.fromWalletId.equals(this.toWalletId)) {
            throw new IllegalArgumentException("Source and destination wallets cannot be the same");
        }
        if (this.amount == null || this.amount <= 0) {
            throw new IllegalArgumentException("Transfer amount must be positive");
        }
    }

    /**
     * Marks the transfer as processed (state transition).
     */
    public void markProcessed() {
        validateCanProcess();
        this.status = TransferStatus.PROCESSED;
    }

    /**
     * Marks the transfer as failed (state transition).
     */
    public void markFailed() {
        validateCanFail();
        this.status = TransferStatus.FAILED;
    }

    public enum TransferStatus {
        PENDING,
        PROCESSED,
        FAILED
    }
}

