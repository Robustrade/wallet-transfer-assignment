package com.example.wallet.exception;

public class WalletNotActiveException extends ApplicationException {

    public WalletNotActiveException(String walletId) {
        super("Wallet not active: " + walletId);
    }
}