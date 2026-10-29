package com.example.wallet.domain;

/** The same idempotency key was sent with a different payload; replaying would be wrong. */
public class IdempotencyKeyReusedException extends RuntimeException {
    public IdempotencyKeyReusedException(String key) {
        super("Idempotency key '" + key + "' was already used with a different request");
    }
}
