package com.wallettransfer.wallet.service;

public class WalletNotFoundException extends RuntimeException {

    private final String walletId;

    public WalletNotFoundException(String walletId) {
        super("Wallet not found: " + walletId);
        this.walletId = walletId;
    }

    public String getWalletId() {
        return walletId;
    }
}
