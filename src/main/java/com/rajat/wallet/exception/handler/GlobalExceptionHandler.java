package com.rajat.wallet.exception.handler;

import com.rajat.wallet.dto.ErrorResponse;
import com.rajat.wallet.exception.DataIntegrityViolations;
import com.rajat.wallet.exception.IdempotencyConflictException;
import com.rajat.wallet.exception.WalletNotFoundException;
import java.util.LinkedHashMap;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.method.annotation.MethodArgumentTypeMismatchException;

/** Translates domain/transport exceptions into the uniform {@link ErrorResponse} + HTTP status. */
@RestControllerAdvice
public class GlobalExceptionHandler {

  private static final Logger log = LoggerFactory.getLogger(GlobalExceptionHandler.class);

  /** Bean-validation failures on the request body (incl. the self-transfer check) -> 400. */
  @ExceptionHandler(MethodArgumentNotValidException.class)
  public ResponseEntity<ErrorResponse> handleValidation(MethodArgumentNotValidException ex) {
    Map<String, String> fieldErrors = new LinkedHashMap<>();
    ex.getBindingResult()
        .getFieldErrors()
        .forEach(fe -> fieldErrors.putIfAbsent(fe.getField(), fe.getDefaultMessage()));
    ex.getBindingResult()
        .getGlobalErrors()
        .forEach(ge -> fieldErrors.putIfAbsent(ge.getObjectName(), ge.getDefaultMessage()));
    return build(HttpStatus.BAD_REQUEST, "Validation failed", fieldErrors);
  }

  /** Unparseable / missing JSON body -> 400. */
  @ExceptionHandler(HttpMessageNotReadableException.class)
  public ResponseEntity<ErrorResponse> handleUnreadable(HttpMessageNotReadableException ex) {
    return build(HttpStatus.BAD_REQUEST, "Malformed request body", null);
  }

  /** A path/query parameter that cannot be bound (e.g. a malformed UUID) -> 400. */
  @ExceptionHandler(MethodArgumentTypeMismatchException.class)
  public ResponseEntity<ErrorResponse> handleTypeMismatch(MethodArgumentTypeMismatchException ex) {
    return build(
        HttpStatus.BAD_REQUEST, "Invalid value for parameter '" + ex.getName() + "'", null);
  }

  @ExceptionHandler(WalletNotFoundException.class)
  public ResponseEntity<ErrorResponse> handleWalletNotFound(WalletNotFoundException ex) {
    return build(HttpStatus.NOT_FOUND, ex.getMessage(), null);
  }

  @ExceptionHandler(IdempotencyConflictException.class)
  public ResponseEntity<ErrorResponse> handleConflict(IdempotencyConflictException ex) {
    return build(HttpStatus.CONFLICT, ex.getMessage(), null);
  }

  /**
   * Only the {@code idempotency_key} unique violation is a retryable {@code 409}: a concurrent
   * first request lost the unique-index race (its transaction rolled back, so no double-apply) and
   * a retry will get the replay. Any other integrity violation (FK, CHECK, NOT NULL) is unexpected
   * — it signals a bug or bad data, not a duplicate — so it is logged and surfaced as {@code 500}
   * rather than misdiagnosed as a retryable conflict.
   */
  @ExceptionHandler(DataIntegrityViolationException.class)
  public ResponseEntity<ErrorResponse> handleDataIntegrity(DataIntegrityViolationException ex) {
    if (DataIntegrityViolations.isIdempotencyKeyViolation(ex)) {
      return build(HttpStatus.CONFLICT, "Concurrent duplicate request; please retry", null);
    }
    log.error("Unexpected data integrity violation", ex);
    return build(HttpStatus.INTERNAL_SERVER_ERROR, "Unexpected error", null);
  }

  /** Defensive guard for stray illegal arguments not caught by bean validation -> 400. */
  @ExceptionHandler(IllegalArgumentException.class)
  public ResponseEntity<ErrorResponse> handleIllegalArgument(IllegalArgumentException ex) {
    return build(HttpStatus.BAD_REQUEST, ex.getMessage(), null);
  }

  @ExceptionHandler(Exception.class)
  public ResponseEntity<ErrorResponse> handleUnexpected(Exception ex) {
    log.error("Unhandled exception", ex);
    return build(HttpStatus.INTERNAL_SERVER_ERROR, "Unexpected error", null);
  }

  private ResponseEntity<ErrorResponse> build(
      HttpStatus status, String message, Map<String, String> fieldErrors) {
    ErrorResponse body =
        ErrorResponse.of(status.value(), status.getReasonPhrase(), message, fieldErrors);
    return ResponseEntity.status(status).body(body);
  }
}
