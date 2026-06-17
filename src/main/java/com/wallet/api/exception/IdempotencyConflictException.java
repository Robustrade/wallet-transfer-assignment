package com.wallet.api.exception;

public class IdempotencyConflictException extends RuntimeException {

    public IdempotencyConflictException(String idempotencyKey) {
        super("Idempotency key reused with a different request: " + idempotencyKey);
    }
}
