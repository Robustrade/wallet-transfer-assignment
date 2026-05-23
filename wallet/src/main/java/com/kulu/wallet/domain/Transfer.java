package com.kulu.wallet.domain;

import java.time.LocalDateTime;

public record Transfer(
    String id,
    String idempotencyKey,
    long fromWalletId,
    long toWalletId,
    long amount,
    TransferStatus status,
    String failureReason,
    LocalDateTime createdAt,
    LocalDateTime updatedAt
) {
    public enum TransferStatus { PENDING, PROCESSED, FAILED }
}
