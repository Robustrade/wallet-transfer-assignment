package com.wallet.dto;

import com.wallet.domain.Wallet;
import java.math.BigDecimal;
import java.time.Instant;

public record WalletResponse(
    String id,
    BigDecimal balance,
    Instant updatedAt
) {
    public static WalletResponse from(Wallet wallet) {
        return new WalletResponse(
            wallet.getId(),
            wallet.getBalance(),
            wallet.getUpdatedAt()
        );
    }
}
