package com.wallet.wallet.dto;

public record TransferRequestDTO(
        String idempotencyKey,
        int fromWalletId,
        int toWalletId,
        double amount
) {
}
