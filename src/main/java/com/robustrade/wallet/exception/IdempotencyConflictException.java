package com.robustrade.wallet.exception;

/** Same idempotency key, different request body. A different request is never a replay. */
public class IdempotencyConflictException extends RuntimeException {

  public IdempotencyConflictException(String idempotencyKey) {
    super("Idempotency key already used for a different request: " + idempotencyKey);
  }
}
