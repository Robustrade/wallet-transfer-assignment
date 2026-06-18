package com.wallet.exception;

public class IdempotencyConflictException extends RuntimeException {

    public IdempotencyConflictException(String idempotencyKey) {
        super("Idempotency key reused with a different request payload: " + idempotencyKey);
    }
}
