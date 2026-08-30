package com.example.wallettransfer.exception;

public class WalletNotFoundException extends RuntimeException {

    public WalletNotFoundException(String walletId) {
        super("Wallet '" + walletId + "' was not found");
    }
}
