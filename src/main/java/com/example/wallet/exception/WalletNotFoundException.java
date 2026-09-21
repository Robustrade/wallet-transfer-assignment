package com.example.wallet.exception;

public class WalletNotFoundException extends ApplicationException {

    public WalletNotFoundException(String walletId) {
        super("Wallet not found: " + walletId);
    }
}