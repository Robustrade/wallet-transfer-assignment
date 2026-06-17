package com.assignment.wallet_transfer.exception;

public class InsufficientBalanceException extends RuntimeException {


    public InsufficientBalanceException(String message) {
        super(message);

    }
}
