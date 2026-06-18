package com.wallet.domain;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import lombok.Getter;

@Getter
public class Transfer {

    private final UUID id;
    private final String idempotencyKey;
    private final String requestFingerprint;
    private final UUID fromWalletId;
    private final UUID toWalletId;
    private final BigDecimal amount;
    private TransferStatus status;
    private String failureReason;
    private final Instant createdAt;
    private Instant updatedAt;

    public Transfer(UUID id, String idempotencyKey, String requestFingerprint, UUID fromWalletId,
            UUID toWalletId, BigDecimal amount, TransferStatus status, String failureReason,
            Instant createdAt, Instant updatedAt) {
        if (fromWalletId.equals(toWalletId)) {
            throw new IllegalArgumentException("fromWalletId and toWalletId must differ");
        }
        BigDecimal normalizedAmount = Money.normalize(amount);
        if (normalizedAmount.signum() <= 0) {
            throw new IllegalArgumentException("amount must be positive");
        }
        this.id = id;
        this.idempotencyKey = idempotencyKey;
        this.requestFingerprint = requestFingerprint;
        this.fromWalletId = fromWalletId;
        this.toWalletId = toWalletId;
        this.amount = normalizedAmount;
        this.status = status;
        this.failureReason = failureReason;
        this.createdAt = createdAt;
        this.updatedAt = updatedAt;
    }

    public static Transfer createPending(UUID id, String idempotencyKey, String requestFingerprint,
            UUID fromWalletId, UUID toWalletId, BigDecimal amount, Instant now) {
        return new Transfer(id, idempotencyKey, requestFingerprint, fromWalletId, toWalletId, amount,
                TransferStatus.PENDING, null, now, now);
    }

    public void markProcessed(Instant now) {
        transitionTo(TransferStatus.PROCESSED, now);
        this.failureReason = null;
    }

    public void markFailed(String reason, Instant now) {
        transitionTo(TransferStatus.FAILED, now);
        this.failureReason = reason;
    }

    private void transitionTo(TransferStatus next, Instant now) {
        if (!status.canTransitionTo(next)) {
            throw new IllegalStateException("Cannot transition transfer from " + status + " to " + next);
        }
        this.status = next;
        this.updatedAt = now;
    }
}
