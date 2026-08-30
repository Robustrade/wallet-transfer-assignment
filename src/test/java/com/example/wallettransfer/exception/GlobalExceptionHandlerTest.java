package com.example.wallettransfer.exception;

import org.junit.jupiter.api.Test;
import org.springframework.http.HttpStatus;
import org.springframework.validation.BeanPropertyBindingResult;
import org.springframework.validation.ObjectError;
import org.springframework.validation.FieldError;
import org.springframework.web.bind.MethodArgumentNotValidException;

import java.util.Map;

import static org.assertj.core.api.Assertions.assertThat;

class GlobalExceptionHandlerTest {

    private final GlobalExceptionHandler handler = new GlobalExceptionHandler();

    @Test
    void mapsDomainExceptionsToStableHttpErrorContracts() {
        assertError(handler.handleIdempotencyConflict(new IdempotencyConflictException("key")),
                HttpStatus.CONFLICT, "IDEMPOTENCY_CONFLICT");
        assertError(handler.handleWalletNotFound(new WalletNotFoundException("wallet")),
                HttpStatus.NOT_FOUND, "WALLET_NOT_FOUND");
        assertError(handler.handleInvalidTransfer(new InvalidTransferException("invalid")),
                HttpStatus.BAD_REQUEST, "INVALID_TRANSFER");
    }

    @Test
    void mapsValidationErrorsAndKeepsFirstMessageForDuplicateFieldErrors() {
        BeanPropertyBindingResult result = new BeanPropertyBindingResult(new Object(), "request");
        result.addError(new FieldError("request", "amount", "must be positive"));
        result.addError(new FieldError("request", "amount", "ignored duplicate"));
        result.addError(new ObjectError("request", "object error"));
        MethodArgumentNotValidException exception = new MethodArgumentNotValidException(null, result);

        var response = handler.handleValidation(exception);

        assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
        assertThat(response.getBody().code()).isEqualTo("VALIDATION_ERROR");
        assertThat(response.getBody().fieldErrors()).isEqualTo(Map.of("amount", "must be positive"));
        assertThat(response.getBody().timestamp()).isNotNull();
    }

    private void assertError(org.springframework.http.ResponseEntity<ApiError> response,
                             HttpStatus expectedStatus, String expectedCode) {
        assertThat(response.getStatusCode()).isEqualTo(expectedStatus);
        assertThat(response.getBody().status()).isEqualTo(expectedStatus.value());
        assertThat(response.getBody().code()).isEqualTo(expectedCode);
        assertThat(response.getBody().fieldErrors()).isEmpty();
        assertThat(response.getBody().timestamp()).isNotNull();
    }
}
