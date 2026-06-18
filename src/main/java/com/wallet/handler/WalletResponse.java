package com.wallet.handler;

import java.math.BigDecimal;
import java.util.UUID;

import com.wallet.domain.Wallet;

public record WalletResponse(UUID walletId, BigDecimal balance) {

    public static WalletResponse from(Wallet wallet) {
        return new WalletResponse(wallet.getId(), wallet.getBalance());
    }
}
