package com.wallettransfer.transfer.dto;

import com.wallettransfer.transfer.domain.TransferStatus;
import java.time.Instant;
import java.util.UUID;

public record TransferResponse(
        UUID id,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferStatus status,
        String failureReason,
        boolean duplicate,
        Instant createdAt
) {
}
