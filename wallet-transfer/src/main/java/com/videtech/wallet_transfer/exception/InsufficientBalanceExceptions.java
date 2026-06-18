package com.videtech.wallet_transfer.exception;

public class InsufficientBalanceExceptions extends RuntimeException {
    public InsufficientBalanceExceptions(String message) {
        super(message);
    }
}
