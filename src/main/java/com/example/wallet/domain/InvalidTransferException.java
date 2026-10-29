package com.example.wallet.domain;

/** The request violates a business invariant (non-positive amount, same wallet, ...). */
public class InvalidTransferException extends RuntimeException {
    public InvalidTransferException(String message) {
        super(message);
    }
}
