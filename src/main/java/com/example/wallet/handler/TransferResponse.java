package com.example.wallet.handler;

import com.example.wallet.domain.Transfer;
import java.time.Instant;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        String status,
        String failureReason,
        Instant createdAt,
        Instant updatedAt) {

    static TransferResponse from(Transfer t) {
        return new TransferResponse(
                t.id(), t.idempotencyKey(), t.fromWalletId(), t.toWalletId(), t.amount(),
                t.status().name(), t.failureReason(), t.createdAt(), t.updatedAt());
    }
}
