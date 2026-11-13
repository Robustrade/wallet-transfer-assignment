package com.wallettransfer.web;

import com.wallettransfer.domain.IdempotencyKeyReusedException;
import com.wallettransfer.domain.TransferNotFoundException;
import com.wallettransfer.domain.ValidationException;
import com.wallettransfer.domain.WalletAlreadyExistsException;
import com.wallettransfer.domain.WalletNotFoundException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.HttpStatusCode;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.ErrorResponse;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

/** Maps exceptions to the documented status codes and error body. */
@RestControllerAdvice
public class GlobalExceptionHandler {

  private static final Logger LOG = LoggerFactory.getLogger(GlobalExceptionHandler.class);

  @ExceptionHandler(ValidationException.class)
  ResponseEntity<ApiError> validation(ValidationException e) {
    return body(HttpStatus.BAD_REQUEST, "VALIDATION_ERROR", e.getMessage());
  }

  /** Malformed JSON, unknown fields, wrongly typed values, empty body. Details are not echoed. */
  @ExceptionHandler(HttpMessageNotReadableException.class)
  ResponseEntity<ApiError> unreadable(HttpMessageNotReadableException e) {
    LOG.debug("Unreadable request body", e);
    return body(HttpStatus.BAD_REQUEST, "INVALID_REQUEST", "request body is missing or invalid");
  }

  @ExceptionHandler(WalletNotFoundException.class)
  ResponseEntity<ApiError> walletNotFound(WalletNotFoundException e) {
    return body(HttpStatus.NOT_FOUND, "WALLET_NOT_FOUND", e.getMessage());
  }

  @ExceptionHandler(TransferNotFoundException.class)
  ResponseEntity<ApiError> transferNotFound(TransferNotFoundException e) {
    return body(HttpStatus.NOT_FOUND, "TRANSFER_NOT_FOUND", e.getMessage());
  }

  @ExceptionHandler(IdempotencyKeyReusedException.class)
  ResponseEntity<ApiError> keyReused(IdempotencyKeyReusedException e) {
    return body(HttpStatus.CONFLICT, "IDEMPOTENCY_KEY_REUSED", e.getMessage());
  }

  @ExceptionHandler(WalletAlreadyExistsException.class)
  ResponseEntity<ApiError> walletExists(WalletAlreadyExistsException e) {
    return body(HttpStatus.CONFLICT, "WALLET_ALREADY_EXISTS", e.getMessage());
  }

  /**
   * Spring's own request errors (405, 415, ...) keep their status; anything else is an opaque 500
   * so internals never leak.
   */
  @ExceptionHandler(Exception.class)
  ResponseEntity<ApiError> fallback(Exception e) {
    if (e instanceof ErrorResponse springError) {
      HttpStatusCode status = springError.getStatusCode();
      return ResponseEntity.status(status)
          .body(ApiError.of("HTTP_" + status.value(), "request rejected"));
    }
    LOG.error("Unhandled exception", e);
    return body(HttpStatus.INTERNAL_SERVER_ERROR, "INTERNAL_ERROR", "internal server error");
  }

  private static ResponseEntity<ApiError> body(HttpStatus status, String code, String message) {
    return ResponseEntity.status(status).body(ApiError.of(code, message));
  }
}
