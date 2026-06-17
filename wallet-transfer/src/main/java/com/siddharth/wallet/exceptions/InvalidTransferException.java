package com.siddharth.wallet.exceptions;

public class InvalidTransferException
        extends RuntimeException {

    public InvalidTransferException(String message) {
        super(message);
    }
}
