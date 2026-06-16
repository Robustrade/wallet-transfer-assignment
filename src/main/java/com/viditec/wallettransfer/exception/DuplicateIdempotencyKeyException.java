package com.viditec.wallettransfer.exception;

public class DuplicateIdempotencyKeyException extends TransferException {

    public DuplicateIdempotencyKeyException() {
        super("IDEMPOTENCY_KEY_REUSED_WITH_DIFFERENT_PAYLOAD",
                "The idempotency key was already used with a different request payload.");
    }
}
