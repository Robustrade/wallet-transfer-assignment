package com.wallet.exception;


public class IdempotencyKeyReusedException extends RuntimeException {
    public IdempotencyKeyReusedException(String idempotencyKey) {
        super("idempotencyKey '" + idempotencyKey + "' was already used with a different request payload");
    }
}
