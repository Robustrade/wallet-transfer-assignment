package com.wallettransfer.domain;

/** A request violates a domain rule (maps to HTTP 400). */
public class ValidationException extends RuntimeException {

  private static final long serialVersionUID = 1L;

  public ValidationException(String message) {
    super(message);
  }
}
