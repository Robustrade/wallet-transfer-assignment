package com.robustrade.wallet.exception;

import com.robustrade.wallet.dto.ErrorResponse;
import java.util.HashMap;
import java.util.Map;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.dao.CannotAcquireLockException;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.HttpStatusCode;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.context.request.WebRequest;
import org.springframework.web.servlet.mvc.method.annotation.ResponseEntityExceptionHandler;

/**
 * Extends {@link ResponseEntityExceptionHandler} so Spring's own mappings for malformed requests
 * still apply. Without it the catch-all below turns every 4xx into a 500, which tells the caller to
 * retry something that can never succeed.
 */
@RestControllerAdvice
public class GlobalExceptionHandler extends ResponseEntityExceptionHandler {

  private static final Logger log = LoggerFactory.getLogger(GlobalExceptionHandler.class);

  @Override
  protected ResponseEntity<Object> handleMethodArgumentNotValid(
      MethodArgumentNotValidException e,
      HttpHeaders headers,
      HttpStatusCode status,
      WebRequest request) {
    Map<String, String> details = new HashMap<>();
    e.getBindingResult()
        .getFieldErrors()
        .forEach(
            error ->
                details.merge(
                    error.getField(),
                    error.getDefaultMessage(),
                    (first, next) -> first + "; " + next));
    log.info("Request rejected as invalid: {}", details);
    return ResponseEntity.badRequest()
        .body(new ErrorResponse("VALIDATION_FAILED", "Request is invalid", details));
  }

  /**
   * Everything Spring rejects before the controller runs: unreadable body, missing header, wrong
   * method or content type, unknown path. The status is Spring's; only the body shape is ours.
   */
  @Override
  protected ResponseEntity<Object> handleExceptionInternal(
      Exception e, Object body, HttpHeaders headers, HttpStatusCode status, WebRequest request) {
    String code = status instanceof HttpStatus known ? known.name() : "REQUEST_REJECTED";
    log.info("Request rejected: {} {}", status.value(), e.getMessage());
    return ResponseEntity.status(status)
        .headers(headers)
        .body(ErrorResponse.of(code, e.getMessage()));
  }

  @ExceptionHandler(WalletNotFoundException.class)
  public ResponseEntity<ErrorResponse> handleWalletNotFound(WalletNotFoundException e) {
    log.info("Transfer rejected: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.NOT_FOUND)
        .body(ErrorResponse.of("WALLET_NOT_FOUND", e.getMessage()));
  }

  @ExceptionHandler(IdempotencyConflictException.class)
  public ResponseEntity<ErrorResponse> handleIdempotencyConflict(IdempotencyConflictException e) {
    // Almost always a client bug: a key was reused for a different payload.
    log.warn("Idempotency conflict: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.CONFLICT)
        .body(ErrorResponse.of("IDEMPOTENCY_CONFLICT", e.getMessage()));
  }

  /**
   * lock_timeout fired (SQLSTATE 55P03). The transaction rolled back before writing anything, so
   * retrying with the same idempotency key is safe.
   */
  @ExceptionHandler(CannotAcquireLockException.class)
  public ResponseEntity<ErrorResponse> handleLockTimeout(CannotAcquireLockException e) {
    log.warn("Could not acquire wallet lock", e);
    return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
        .header("Retry-After", "1")
        .body(ErrorResponse.of("LOCK_TIMEOUT", "Wallet is busy, retry with the same key"));
  }

  @ExceptionHandler(Exception.class)
  public ResponseEntity<ErrorResponse> handleUnexpected(Exception e) {
    log.error("Unexpected error handling request", e);
    return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
        .body(ErrorResponse.of("INTERNAL_ERROR", "Something went wrong, retry with the same key"));
  }
}
