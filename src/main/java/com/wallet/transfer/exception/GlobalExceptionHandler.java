package com.wallet.transfer.exception;

import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.validation.FieldError;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.util.HashMap;
import java.util.Map;

/**
 * Global exception handler that intercepts thrown exceptions and converts them
 * into standardized, client-friendly HTTP responses.
 */
@RestControllerAdvice
public class GlobalExceptionHandler {

    /**
     * Handles cases where a requested wallet ID does not exist in the database.
     * Returns HTTP 404 (Not Found).
     */
    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<Map<String, Object>> handleWalletNotFound(WalletNotFoundException ex) {
        return buildResponse(HttpStatus.NOT_FOUND, ex.getMessage());
    }

    /**
     * Handles transactions where the source wallet does not have enough balance.
     * Returns HTTP 422 (Unprocessable Entity) as it is a semantic validation failure.
     */
    @ExceptionHandler(InsufficientFundsException.class)
    public ResponseEntity<Map<String, Object>> handleInsufficientFunds(InsufficientFundsException ex) {
        return buildResponse(HttpStatus.UNPROCESSABLE_ENTITY, ex.getMessage());
    }

    /**
     * Handles attempts to transfer funds to the same wallet.
     * Returns HTTP 400 (Bad Request).
     */
    @ExceptionHandler(SelfTransferException.class)
    public ResponseEntity<Map<String, Object>> handleSelfTransfer(SelfTransferException ex) {
        return buildResponse(HttpStatus.BAD_REQUEST, ex.getMessage());
    }

    /**
     * Handles transfer requests with negative or zero amounts.
     * Returns HTTP 400 (Bad Request).
     */
    @ExceptionHandler(InvalidAmountException.class)
    public ResponseEntity<Map<String, Object>> handleInvalidAmount(InvalidAmountException ex) {
        return buildResponse(HttpStatus.BAD_REQUEST, ex.getMessage());
    }

    /**
     * Handles Spring validation errors (e.g., missing fields or invalid inputs on TransferRequest).
     * Extracts field errors and returns a detailed HTTP 400 (Bad Request) map.
     */
    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<Map<String, Object>> handleValidationExceptions(MethodArgumentNotValidException ex) {
        Map<String, String> errors = new HashMap<>();
        ex.getBindingResult().getAllErrors().forEach((error) -> {
            String fieldName = ((FieldError) error).getField();
            String errorMessage = error.getDefaultMessage();
            errors.put(fieldName, errorMessage);
        });

        Map<String, Object> body = new HashMap<>();
        body.put("status", HttpStatus.BAD_REQUEST.value());
        body.put("error", "Validation Failed");
        body.put("details", errors);
        return new ResponseEntity<>(body, HttpStatus.BAD_REQUEST);
    }

    /**
     * Handles user validation or authentication exceptions (e.g., username already exists, invalid credentials).
     * Returns HTTP 400 (Bad Request).
     */
    @ExceptionHandler(UserExceptionHandler.class)
    public ResponseEntity<Map<String, Object>> handleUserException(UserExceptionHandler ex) {
        return buildResponse(HttpStatus.BAD_REQUEST, ex.getMessage());
    }

    /**
     * Fallback handler for any uncaught system exceptions.
     * Returns HTTP 500 (Internal Server Error) to prevent leaking raw system trace information.
     */
    @ExceptionHandler(Exception.class)
    public ResponseEntity<Map<String, Object>> handleGeneralException(Exception ex) {
        return buildResponse(HttpStatus.INTERNAL_SERVER_ERROR, "An unexpected error occurred: " + ex.getMessage());
    }

    /**
     * Utility method to construct standardized JSON error response bodies.
     */
    private ResponseEntity<Map<String, Object>> buildResponse(HttpStatus status, String message) {
        Map<String, Object> body = new HashMap<>();
        body.put("status", status.value());
        body.put("error", message);
        return new ResponseEntity<>(body, status);
    }
}
