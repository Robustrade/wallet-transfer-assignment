package com.wallet.transferservice.exception;

import java.util.UUID;

public class WalletNotFoundException extends RuntimeException {
    private final UUID walletId;

    public WalletNotFoundException(UUID walletId) {
        super("Wallet not found: " + walletId);
        this.walletId = walletId;
    }

    public UUID getWalletId() {
        return walletId;
    }
}
