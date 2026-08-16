package com.api.wallet.domain;

import jakarta.persistence.Column;
import jakarta.persistence.Entity;
import jakarta.persistence.EnumType;
import jakarta.persistence.Enumerated;
import jakarta.persistence.Id;
import jakarta.persistence.Table;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;
import lombok.AccessLevel;
import lombok.Getter;
import lombok.NoArgsConstructor;

@Entity
@Table(name = "transfers")
@Getter
@NoArgsConstructor(access = AccessLevel.PROTECTED)
public class Transfer {

    @Id
    @Column(name = "id", nullable = false, updatable = false)
    private UUID id;

    @Column(name = "idempotency_key", nullable = false, updatable = false)
    private String idempotencyKey;

    @Column(name = "from_wallet_id", nullable = false, updatable = false)
    private String fromWalletId;

    @Column(name = "to_wallet_id", nullable = false, updatable = false)
    private String toWalletId;

    @Column(name = "amount", nullable = false, updatable = false)
    private BigDecimal amount;

    @Enumerated(EnumType.STRING)
    @Column(name = "status", nullable = false)
    private TransferStatus status;

    @Column(name = "failure_reason")
    private String failureReason;

    @Column(name = "created_at", nullable = false, updatable = false)
    private Instant createdAt;

    @Column(name = "updated_at", nullable = false)
    private Instant updatedAt;

    public static Transfer initiate(
            String idempotencyKey, String fromWalletId, String toWalletId, BigDecimal amount) {
        if (amount == null || amount.compareTo(BigDecimal.ZERO) <= 0) {
            throw new IllegalArgumentException("Transfer amount must be positive");
        }
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must differ");
        }
        Transfer transfer = new Transfer();
        transfer.id = UUID.randomUUID();
        transfer.idempotencyKey = idempotencyKey;
        transfer.fromWalletId = fromWalletId;
        transfer.toWalletId = toWalletId;
        transfer.amount = amount;
        transfer.status = TransferStatus.PENDING;
        transfer.createdAt = Instant.now();
        transfer.updatedAt = transfer.createdAt;
        return transfer;
    }

    public void markProcessed() {
        transitionTo(TransferStatus.PROCESSED);
    }

    public void markFailed(String reason) {
        transitionTo(TransferStatus.FAILED);
        this.failureReason = reason;
    }

    private void transitionTo(TransferStatus next) {
        if (!this.status.canTransit(next)) {
            throw new IllegalStateException(
                    "Cannot transition transfer %s from %s to %s".formatted(id, status, next));
        }
        this.status = next;
        this.updatedAt = Instant.now();
    }
}
