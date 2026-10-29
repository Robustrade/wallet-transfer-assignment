package com.example.wallet.domain;

import java.time.Instant;
import java.util.UUID;

/**
 * Immutable transfer snapshot. State changes return a new instance, and illegal transitions are
 * rejected here so the rule lives in the domain rather than in SQL or the service.
 */
public record Transfer(
        UUID id,
        String idempotencyKey,
        String requestHash,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferStatus status,
        String failureReason,
        Instant createdAt,
        Instant updatedAt) {

    public static Transfer pending(
            String idempotencyKey, String requestHash, String fromWalletId, String toWalletId, long amount) {
        if (amount <= 0) {
            throw new InvalidTransferException("amount must be positive");
        }
        if (fromWalletId.equals(toWalletId)) {
            throw new InvalidTransferException("source and destination wallets must differ");
        }
        Instant now = Instant.now();
        return new Transfer(
                UUID.randomUUID(),
                idempotencyKey,
                requestHash,
                fromWalletId,
                toWalletId,
                amount,
                TransferStatus.PENDING,
                null,
                now,
                now);
    }

    public Transfer markProcessed() {
        requireTransitionTo(TransferStatus.PROCESSED);
        return withStatus(TransferStatus.PROCESSED, null);
    }

    public Transfer markFailed(String reason) {
        requireTransitionTo(TransferStatus.FAILED);
        return withStatus(TransferStatus.FAILED, reason);
    }

    private void requireTransitionTo(TransferStatus next) {
        if (!status.canTransitionTo(next)) {
            throw new IllegalTransferStateException(id, status, next);
        }
    }

    private Transfer withStatus(TransferStatus next, String reason) {
        return new Transfer(
                id, idempotencyKey, requestHash, fromWalletId, toWalletId, amount, next, reason, createdAt,
                Instant.now());
    }
}
