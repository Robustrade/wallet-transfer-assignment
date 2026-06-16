package com.viditec.wallettransfer.dto;

public record CreateTransferDto(
        String idempotencyKey,
        String fromWalletId,
        String toWalletId,
        long amount
) {
}
