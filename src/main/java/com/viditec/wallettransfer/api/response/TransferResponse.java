package com.viditec.wallettransfer.api.response;

import com.viditec.wallettransfer.domain.TransferState;

public record TransferResponse(
        String transferId,
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount,
        TransferState state
) {
}
