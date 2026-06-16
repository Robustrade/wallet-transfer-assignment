package com.wallet.exception;

/**
 * Thrown when an idempotency key is currently being processed by another request.
 */
public class IdempotencyKeyInProgressException extends RuntimeException {

    private final String idempotencyKey;

    public IdempotencyKeyInProgressException(String idempotencyKey) {
        super("Idempotency key '" + idempotencyKey + "' is already in progress");
        this.idempotencyKey = idempotencyKey;
    }

    public String getIdempotencyKey() {
        return idempotencyKey;
    }
}
