package com.wallet.transferservice.dto;

import java.time.Instant;

/**
 * Successful transfer response, also stored in idempotency_records.response_body
 * for replay.
 */
public record TransferResponse(
        String transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        String amount,
        String status,
        Instant createdAt
) {
}
