package com.example.wallet.domain;

public class WalletNotFoundException extends RuntimeException {
    public WalletNotFoundException(String walletId) {
        super("Wallet not found: " + walletId);
    }
}
