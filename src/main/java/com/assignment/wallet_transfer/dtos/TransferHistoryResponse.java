package com.assignment.wallet_transfer.dtos;

import com.assignment.wallet_transfer.enums.TransferStatus;

import java.math.BigDecimal;
import java.time.LocalDateTime;
import java.util.UUID;

public record TransferHistoryResponse(
        UUID transferId,
        UUID sourceWalletId,
        UUID destinationWalletId,
        BigDecimal amount,
        TransferStatus status,
        LocalDateTime createdAt
) {

}
