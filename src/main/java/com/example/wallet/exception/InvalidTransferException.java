package com.example.wallet.exception;

public class InvalidTransferException extends BusinessException {

    public InvalidTransferException(String message) {
        super(message);
    }
}