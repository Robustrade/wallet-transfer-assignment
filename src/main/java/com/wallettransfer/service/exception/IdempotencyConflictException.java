package com.wallettransfer.service.exception;

/**
 * Thrown when the same idempotency key is reused with a different request payload.
 */
public class IdempotencyConflictException extends RuntimeException {

    /**
     * @param message detail for API error response (HTTP 409)
     */
    public IdempotencyConflictException(String message) {
        super(message);
    }
}
