package com.walletservice.application.exception;

/**
 * Exception thrown when an idempotency key is reused with different request parameters.
 */
public class IdempotencyConflictException extends RuntimeException {
    public IdempotencyConflictException(String message) {
        super(message);
    }

    public IdempotencyConflictException(String message, Throwable cause) {
        super(message, cause);
    }
}

