package com.kullu.wallet.exception;

import lombok.Getter;

@Getter
public class IdempotencyConflictException extends RuntimeException {

    public enum Kind {
        PAYLOAD_MISMATCH,
        IN_FLIGHT
    }

    private final Kind kind;

    public IdempotencyConflictException(final Kind kind, final String message) {
        super(message);
        this.kind = kind;
    }
}
