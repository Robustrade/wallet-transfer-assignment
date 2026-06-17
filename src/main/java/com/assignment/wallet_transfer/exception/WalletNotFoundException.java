package com.assignment.wallet_transfer.exception;

public class WalletNotFoundException  extends RuntimeException {

    public WalletNotFoundException(String message) {
        super(message);
    }

}