package com.wallet.dto;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
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
    Instant createdAt
) {
    public static TransferResponse from(Transfer transfer) {
        return new TransferResponse(
            transfer.getId(),
            transfer.getIdempotencyKey(),
            transfer.getFromWalletId(),
            transfer.getToWalletId(),
            transfer.getAmount(),
            transfer.getStatus(),
            transfer.getFailureReason(),
            transfer.getCreatedAt()
        );
    }
}
