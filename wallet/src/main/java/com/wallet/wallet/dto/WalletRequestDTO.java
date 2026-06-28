package com.wallet.wallet.dto;

public record WalletRequestDTO(
        String idempotencyKey,
        String name,
        String mobile,
        String email,
        double amount
) {
}
