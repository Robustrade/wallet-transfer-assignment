package com.wallet.controller.dto;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;

import java.time.Instant;
import java.util.UUID;

public record TransferResponse(
        UUID id,
        String fromWalletId,
        String toWalletId,
        Long amount,
        TransferStatus status,
        String errorMessage,
        Instant createdAt
) {
    public static TransferResponse from(Transfer transfer) {
        return new TransferResponse(
                transfer.getId(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                transfer.getErrorMessage(),
                transfer.getCreatedAt()
        );
    }
}
