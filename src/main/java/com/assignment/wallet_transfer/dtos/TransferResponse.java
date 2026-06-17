package com.assignment.wallet_transfer.dtos;

import com.assignment.wallet_transfer.enums.TransferStatus;

import java.math.BigDecimal;
import java.util.UUID;

public record TransferResponse(
        UUID transferId,
        TransferStatus status,
        BigDecimal amount
) {

}
