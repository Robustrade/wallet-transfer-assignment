package com.wallet.controller.dto;

import java.time.Instant;

public record WalletBalanceResponse(
        String walletId,
        Long balance,
        Instant updatedAt
) {
}
