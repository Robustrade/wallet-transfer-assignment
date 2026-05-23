package com.kulu.wallet.exception;

public class WalletNotFoundException extends RuntimeException {
    public WalletNotFoundException(long walletId) {
        super("Wallet not found: " + walletId);
    }
}
