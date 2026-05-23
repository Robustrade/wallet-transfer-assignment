package com.kulu.wallet.exception;

public class InsufficientFundsException extends RuntimeException {
    public InsufficientFundsException(long walletId) {
        super("Insufficient funds in wallet: " + walletId);
    }
}
