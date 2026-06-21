package com.rajat.wallet.exception;

import java.util.Locale;
import org.hibernate.exception.ConstraintViolationException;
import org.springframework.dao.DataIntegrityViolationException;

/**
 * Classifies {@link DataIntegrityViolationException}s by the constraint they violated. Only the
 * idempotency-key unique violation is a replayable concurrent-duplicate race; every other integrity
 * violation (FK, CHECK, NOT NULL) is an unexpected failure and must not be treated as a retry.
 */
public final class DataIntegrityViolations {

  /** Name of the {@code idempotency_key} unique constraint (see Flyway {@code V2}). */
  public static final String IDEMPOTENCY_KEY_CONSTRAINT = "idempotency_records_key_unique";

  private DataIntegrityViolations() {}

  /** True only when the violation is the {@code idempotency_key} unique constraint. */
  public static boolean isIdempotencyKeyViolation(DataIntegrityViolationException ex) {
    if (ex.getCause() instanceof ConstraintViolationException cve
        && IDEMPOTENCY_KEY_CONSTRAINT.equalsIgnoreCase(cve.getConstraintName())) {
      return true;
    }
    String message = ex.getMostSpecificCause().getMessage();
    return message != null && message.toLowerCase(Locale.ROOT).contains(IDEMPOTENCY_KEY_CONSTRAINT);
  }
}
