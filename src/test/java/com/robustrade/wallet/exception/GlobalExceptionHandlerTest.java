package com.robustrade.wallet.exception;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.dto.ErrorResponse;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.springframework.dao.CannotAcquireLockException;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.validation.BindingResult;
import org.springframework.validation.FieldError;
import org.springframework.web.bind.MethodArgumentNotValidException;

class GlobalExceptionHandlerTest {

  private final GlobalExceptionHandler handler = new GlobalExceptionHandler();

  @Test
  void validationFailuresAreBadRequestWithPerFieldDetails() {
    BindingResult bindingResult = mock(BindingResult.class);
    when(bindingResult.getFieldErrors())
        .thenReturn(
            List.of(
                new FieldError("createTransferRequest", "amount", "must be greater than 0"),
                new FieldError("createTransferRequest", "fromWalletId", "must not be blank")));
    MethodArgumentNotValidException exception = mock(MethodArgumentNotValidException.class);
    when(exception.getBindingResult()).thenReturn(bindingResult);

    ResponseEntity<Object> response =
        handler.handleMethodArgumentNotValid(
            exception, new HttpHeaders(), HttpStatus.BAD_REQUEST, null);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    ErrorResponse body = (ErrorResponse) response.getBody();
    assertThat(body).isNotNull();
    assertThat(body.error()).isEqualTo("VALIDATION_FAILED");
    assertThat(body.details())
        .containsEntry("amount", "must be greater than 0")
        .containsEntry("fromWalletId", "must not be blank");
  }

  @Test
  void validationFailureWithNoFieldErrorsStillReports() {
    BindingResult bindingResult = mock(BindingResult.class);
    when(bindingResult.getFieldErrors()).thenReturn(List.of());
    MethodArgumentNotValidException exception = mock(MethodArgumentNotValidException.class);
    when(exception.getBindingResult()).thenReturn(bindingResult);

    ResponseEntity<Object> response =
        handler.handleMethodArgumentNotValid(
            exception, new HttpHeaders(), HttpStatus.BAD_REQUEST, null);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    ErrorResponse body = (ErrorResponse) response.getBody();
    assertThat(body).isNotNull();
    assertThat(body.details()).isEmpty();
  }

  @Test
  void unknownWalletIsNotFound() {
    ResponseEntity<ErrorResponse> response =
        handler.handleWalletNotFound(new WalletNotFoundException("wallet_9"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.NOT_FOUND);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().error()).isEqualTo("WALLET_NOT_FOUND");
    assertThat(response.getBody().message()).contains("wallet_9");
    assertThat(response.getBody().details()).isNull();
  }

  @Test
  void reusedKeyIsAConflict() {
    ResponseEntity<ErrorResponse> response =
        handler.handleIdempotencyConflict(new IdempotencyConflictException("key-1"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.CONFLICT);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().error()).isEqualTo("IDEMPOTENCY_CONFLICT");
    assertThat(response.getBody().message()).contains("key-1");
  }

  /** Nothing was written before the rollback, so the client is told to retry the same key. */
  @Test
  void lockTimeoutIsRetryableServiceUnavailable() {
    ResponseEntity<ErrorResponse> response =
        handler.handleLockTimeout(new CannotAcquireLockException("55P03"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.SERVICE_UNAVAILABLE);
    assertThat(response.getHeaders().getFirst("Retry-After")).isEqualTo("1");
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().error()).isEqualTo("LOCK_TIMEOUT");
  }

  @Test
  void anythingElseIsAnInternalError() {
    ResponseEntity<ErrorResponse> response = handler.handleUnexpected(new RuntimeException("boom"));

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.INTERNAL_SERVER_ERROR);
    assertThat(response.getBody()).isNotNull();
    assertThat(response.getBody().error()).isEqualTo("INTERNAL_ERROR");
    assertThat(response.getBody().message()).doesNotContain("boom");
  }

  /** A field can break two rules at once; the client needs both messages, not just the last. */
  @Test
  void multipleViolationsOnOneFieldAreAllReported() {
    BindingResult bindingResult = mock(BindingResult.class);
    when(bindingResult.getFieldErrors())
        .thenReturn(
            List.of(
                new FieldError("createTransferRequest", "idempotencyKey", "must not be blank"),
                new FieldError(
                    "createTransferRequest", "idempotencyKey", "size must be at most 255")));
    MethodArgumentNotValidException exception = mock(MethodArgumentNotValidException.class);
    when(exception.getBindingResult()).thenReturn(bindingResult);

    ResponseEntity<Object> response =
        handler.handleMethodArgumentNotValid(
            exception, new HttpHeaders(), HttpStatus.BAD_REQUEST, null);

    ErrorResponse body = (ErrorResponse) response.getBody();
    assertThat(body).isNotNull();
    assertThat(body.details().get("idempotencyKey"))
        .contains("must not be blank")
        .contains("size must be at most 255");
  }
}
