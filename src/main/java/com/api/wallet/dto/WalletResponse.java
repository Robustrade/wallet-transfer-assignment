package com.api.wallet.dto;

import com.api.wallet.domain.Wallet;
import java.math.BigDecimal;
import java.time.Instant;

public record WalletResponse(String id, BigDecimal balance, Instant createdAt) {

    public static WalletResponse from(Wallet wallet) {
        return new WalletResponse(wallet.getId(), wallet.getBalance(), wallet.getCreatedAt());
    }
}
