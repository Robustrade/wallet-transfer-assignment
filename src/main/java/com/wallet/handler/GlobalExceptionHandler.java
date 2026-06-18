package com.wallet.handler;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.HttpStatusCode;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.context.request.WebRequest;
import org.springframework.web.servlet.mvc.method.annotation.ResponseEntityExceptionHandler;

import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.InvalidTransferRequestException;
import com.wallet.exception.TransferNotFoundException;
import com.wallet.exception.WalletNotFoundException;

/**
 * Maps every exception to a consistent {@link ErrorResponse} body. Extends
 * {@link ResponseEntityExceptionHandler} so that Spring's own request exceptions (malformed JSON
 * body, unsupported media type, unsupported method, missing parameters, ...) keep their correct
 * 4xx status codes instead of being collapsed into a 500 by the catch-all handler.
 */
@RestControllerAdvice
public class GlobalExceptionHandler extends ResponseEntityExceptionHandler {

    private static final Logger log = LoggerFactory.getLogger(GlobalExceptionHandler.class);

    @ExceptionHandler(InvalidTransferRequestException.class)
    public ResponseEntity<ErrorResponse> handleInvalidRequest(InvalidTransferRequestException ex) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST).body(ErrorResponse.of(ex.getMessage()));
    }

    @ExceptionHandler({WalletNotFoundException.class, TransferNotFoundException.class})
    public ResponseEntity<ErrorResponse> handleNotFound(RuntimeException ex) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND).body(ErrorResponse.of(ex.getMessage()));
    }

    @ExceptionHandler(IdempotencyConflictException.class)
    public ResponseEntity<ErrorResponse> handleConflict(IdempotencyConflictException ex) {
        return ResponseEntity.status(HttpStatus.CONFLICT).body(ErrorResponse.of(ex.getMessage()));
    }

    /** Bean-validation failures on the request body — report which fields were invalid and why. */
    @Override
    protected ResponseEntity<Object> handleMethodArgumentNotValid(MethodArgumentNotValidException ex,
            HttpHeaders headers, HttpStatusCode status, WebRequest request) {
        String message = ex.getBindingResult().getFieldErrors().stream()
                .map(error -> error.getField() + " " + error.getDefaultMessage())
                .reduce((a, b) -> a + "; " + b)
                .orElse("Validation failed");
        return new ResponseEntity<>(ErrorResponse.of(message), HttpStatus.BAD_REQUEST);
    }

    /**
     * Renders the body for Spring's built-in request exceptions in our {@link ErrorResponse} shape
     * while preserving the status code Spring already chose (a malformed body stays a 400, an
     * unsupported method a 405, and so on). Messages are kept generic so nothing internal leaks.
     */
    @Override
    protected ResponseEntity<Object> handleExceptionInternal(Exception ex, Object body, HttpHeaders headers,
            HttpStatusCode statusCode, WebRequest request) {
        String message = statusCode.is4xxClientError() ? "Invalid request" : "Internal error";
        return new ResponseEntity<>(ErrorResponse.of(message), statusCode);
    }

    /**
     * Anything not handled above is a bug, not a client error. Log it server-side, but never
     * return the exception's own message — that risks leaking internal details to the caller.
     */
    @ExceptionHandler(Exception.class)
    public ResponseEntity<ErrorResponse> handleUnexpected(Exception ex) {
        log.error("Unhandled exception", ex);
        return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR).body(ErrorResponse.of("Internal error"));
    }
}
