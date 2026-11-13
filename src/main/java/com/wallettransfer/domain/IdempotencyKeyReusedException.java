package com.wallettransfer.domain;

/** An idempotency key was reused with a different request payload (maps to HTTP 409). */
public class IdempotencyKeyReusedException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public IdempotencyKeyReusedException(String key) {
    super("idempotency key '" + key + "' was already used with a different request");
  }
}
