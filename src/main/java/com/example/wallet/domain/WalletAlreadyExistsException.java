package com.example.wallet.domain;

public class WalletAlreadyExistsException extends RuntimeException {
    public WalletAlreadyExistsException(String walletId) {
        super("Wallet already exists: " + walletId);
    }
}
