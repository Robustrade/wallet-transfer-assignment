package com.wallet.domain;

import java.time.Instant;
import java.util.UUID;

public record Transfer(
        UUID id,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferStatus status,
        String failureReason,
        Instant createdAt,
        Instant updatedAt) {

    public Transfer withStatus(TransferStatus newStatus, String newFailureReason) {
        return new Transfer(
                id,
                idempotencyKey,
                fromWalletId,
                toWalletId,
                amount,
                newStatus,
                newFailureReason,
                createdAt,
                Instant.now());
    }
}
