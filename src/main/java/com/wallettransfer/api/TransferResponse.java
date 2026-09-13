package com.wallettransfer.api;

import com.wallettransfer.domain.TransferStatus;

import java.math.BigDecimal;
import java.time.OffsetDateTime;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        BigDecimal amount,
        TransferStatus status,
        OffsetDateTime createdAt) {
}