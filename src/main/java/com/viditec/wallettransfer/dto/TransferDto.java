package com.viditec.wallettransfer.dto;

import com.viditec.wallettransfer.domain.TransferState;

public record TransferDto(
        String transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferState state
) {
}
