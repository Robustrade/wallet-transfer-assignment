package com.rajat.wallet.exception;

/**
 * An idempotency key was reused with a different payload, or the original request is still in
 * flight. Mapped to HTTP 409.
 */
public class IdempotencyConflictException extends RuntimeException {

  public IdempotencyConflictException(String message) {
    super(message);
  }
}
