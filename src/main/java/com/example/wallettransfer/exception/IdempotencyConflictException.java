package com.example.wallettransfer.exception;

public class IdempotencyConflictException extends RuntimeException {

    public IdempotencyConflictException(String key) {
        super("Idempotency key '" + key + "' was already used with a different request payload");
    }
}
