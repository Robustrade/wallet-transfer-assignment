package com.example.wallet.api;

import com.example.wallet.domain.Transfer;
import java.math.BigDecimal;
import java.util.UUID;

public record TransferResponse(
    UUID transferId,
    String idempotencyKey,
    String fromWalletId,
    String toWalletId,
    BigDecimal amount,
    String status
) {
    public static TransferResponse from(Transfer t) {
        return new TransferResponse(t.getId(), t.getIdempotencyKey(), t.getFromWalletId(),
            t.getToWalletId(), t.getAmount(), t.getStatus().name());
    }
}
