package com.wallet.dto;

import com.wallet.domain.Transfer;

import java.math.BigDecimal;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        String status,
        String fromWalletId,
        String toWalletId,
        BigDecimal amount,
        String failureReason,
        boolean replayed
) {
    public static TransferResponse from(Transfer transfer, boolean replayed) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getStatus().name(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getFailureReason(),
                replayed
        );
    }
}
