package com.wallet.wallet.dto;

import java.time.LocalDateTime;
import java.util.Date;

public record WalletResponseDTO(
        String name,
        int walletId,
        double amount,
        LocalDateTime createdAt
) {
}
