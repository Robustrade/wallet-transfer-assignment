package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.domain.TransferState;

public record TransferRecord(
        String transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferState state
) {
}
