package com.wallet.transferservice.exception;

public class IdempotencyKeyConflictException extends RuntimeException {
    public IdempotencyKeyConflictException(String idempotencyKey) {
        super("Idempotency key reused with different parameters: " + idempotencyKey);
    }
}
