package com.example.wallettransfer.domain;

import java.util.UUID;

public record Transfer(
        UUID id,
        String idempotencyKey,
        String requestHash,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferState state,
        String failureReason) {
}
