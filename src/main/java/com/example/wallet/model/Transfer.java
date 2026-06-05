package com.example.wallet.model;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

public record Transfer(
        UUID id,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        BigDecimal amount,
        TransferStatus status,
        Instant createdAt,
        Instant updatedAt
) {

    /**
     * Returns a copy of this transfer with the given status.
     * Used to represent state transitions without mutating the record.
     */
    public Transfer withStatus(TransferStatus newStatus) {
        return new Transfer(id, idempotencyKey, fromWalletId, toWalletId, amount, newStatus, createdAt, Instant.now());
    }
}

