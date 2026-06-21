package com.wallet.transfer.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferStatus;
import java.time.Instant;

@JsonInclude(JsonInclude.Include.NON_NULL)
public record TransferResponse(
        String id,
        String fromWalletId,
        String toWalletId,
        Long amount,
        TransferStatus status,
        String failureReason,
        Instant createdAt
) {
    public static TransferResponse from(Transfer transfer) {
        String reason = transfer.getStatus() == TransferStatus.FAILED
                ? "Insufficient funds" : null;
        return new TransferResponse(
                transfer.getId(),
                transfer.getFromWalletId(),
                transfer.getToWalletId(),
                transfer.getAmount(),
                transfer.getStatus(),
                reason,
                transfer.getCreatedAt()
        );
    }
}
