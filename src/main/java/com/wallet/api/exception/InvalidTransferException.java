package com.wallet.api.exception;

public class InvalidTransferException extends RuntimeException {

    public InvalidTransferException(String message) {
        super(message);
    }
}
