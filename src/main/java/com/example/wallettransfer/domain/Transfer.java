package com.example.wallettransfer.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import lombok.Getter;
import lombok.NoArgsConstructor;

import java.math.BigDecimal;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.UUID;

@Entity
@Getter
@NoArgsConstructor(access = lombok.AccessLevel.PROTECTED)
@Table(name = "transfers")
public class Transfer {

    @Id
    @Column(name = "id", nullable = false)
    private UUID id;

    @Column(name = "from_wallet_id", nullable = false, length = 64)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, length = 64)
    private String toWalletId;

    @Column(name = "amount", nullable = false, precision = 19, scale = 4)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false, length = 16)
    private TransferStatus status;

    @Column(name = "failure_reason", length = 255)
    private String failureReason;

    @Column(name = "created_at", nullable = false)
    private Instant createdAt;

    @Column(name = "completed_at")
    private Instant completedAt;

    private Transfer(String fromWalletId, String toWalletId, BigDecimal amount) {
        this.id = UUID.randomUUID();
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = amount;
        this.status = TransferStatus.PENDING;
        this.createdAt = now();
    }

    public static Transfer pending(String fromWalletId, String toWalletId, BigDecimal amount) {
        if (fromWalletId == null || toWalletId == null || fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("Source and destination wallets must be different");
        }
        if (amount == null || amount.signum() <= 0) {
            throw new IllegalArgumentException("Amount must be positive");
        }
        return new Transfer(fromWalletId, toWalletId, amount);
    }

    public void markProcessed() {
        requirePending();
        this.status = TransferStatus.PROCESSED;
        this.failureReason = null;
        this.completedAt = now();
    }

    public void markFailed(String reason) {
        requirePending();
        if (reason == null || reason.isBlank()) {
            throw new IllegalArgumentException("Failure reason is required");
        }
        this.status = TransferStatus.FAILED;
        this.failureReason = reason;
        this.completedAt = now();
    }

    private static Instant now() {
        return Instant.now().truncatedTo(ChronoUnit.MICROS);
    }

    private void requirePending() {
        if (status != TransferStatus.PENDING) {
            throw new IllegalStateException("Transfer in state " + status + " cannot transition again");
        }
    }

}
