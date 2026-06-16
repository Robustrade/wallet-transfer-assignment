package com.wallet.exception;

/**
 * Thrown when an idempotency key is reused with a different request payload.
 */
public class IdempotencyConflictException extends RuntimeException {

    private final String idempotencyKey;

    public IdempotencyConflictException(String idempotencyKey) {
        super("Idempotency key '" + idempotencyKey
                + "' was previously used with a different request payload");
        this.idempotencyKey = idempotencyKey;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }
}
