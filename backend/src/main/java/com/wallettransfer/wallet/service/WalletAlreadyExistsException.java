package com.wallettransfer.wallet.service;

public class WalletAlreadyExistsException extends RuntimeException {

    private final String walletId;

    public WalletAlreadyExistsException(String walletId) {
        super("Wallet already exists: " + walletId);
        this.walletId = walletId;
    }

    public String getWalletId() {
        return walletId;
    }
}
