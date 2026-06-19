package com.wallet.transferservice.exception;

import java.util.UUID;

public class WalletInactiveException extends RuntimeException {
    private final UUID walletId;

    public WalletInactiveException(UUID walletId) {
        super("Wallet is not active: " + walletId);
        this.walletId = walletId;
    }

    public UUID getWalletId() {
        return walletId;
    }
}
