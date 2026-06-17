package com.siddharth.wallet.exceptions;

public class DuplicateTransferException extends RuntimeException {

    public DuplicateTransferException(String idempotencyKey) {
        super("Transfer already exists for idempotency key: " + idempotencyKey);
    }
}