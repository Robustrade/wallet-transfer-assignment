package com.example.wallettransfer.dto;

import com.example.wallettransfer.domain.TransferStatus;

import java.math.BigDecimal;
import java.time.Instant;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        BigDecimal amount,
        TransferStatus status,
        String failureReason,
        Instant createdAt,
        Instant completedAt
) {
}
