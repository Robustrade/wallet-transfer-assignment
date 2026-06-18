package com.wallet.handler;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

import com.wallet.domain.Transfer;

public record TransferResponse(
        UUID transferId,
        String idempotencyKey,
        UUID fromWalletId,
        UUID toWalletId,
        BigDecimal amount,
        String status,
        String failureReason,
        Instant createdAt,
        Instant updatedAt) {

    public static TransferResponse from(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getIdempotencyKey(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus().name(),
                transfer.getFailureReason(),
                transfer.getCreatedAt(),
                transfer.getUpdatedAt());
    }
}
