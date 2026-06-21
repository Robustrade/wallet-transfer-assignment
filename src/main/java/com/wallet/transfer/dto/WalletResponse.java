package com.wallet.transfer.dto;

import com.wallet.transfer.domain.Wallet;
import java.time.Instant;

public record WalletResponse(
        String id,
        Long balance,
        Instant createdAt,
        Instant updatedAt
) {
    public static WalletResponse from(Wallet wallet) {
        return new WalletResponse(
                wallet.getId(),
                wallet.getBalance(),
                wallet.getCreatedAt(),
                wallet.getUpdatedAt()
        );
    }
}
