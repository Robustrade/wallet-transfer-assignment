package com.example.wallet.handler;

import com.example.wallet.exception.InsufficientFundsException;
import com.example.wallet.exception.InvalidTransferStateException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.handler.dto.ErrorResponse;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.validation.FieldError;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@Slf4j
@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(InsufficientFundsException.class)
    public ResponseEntity<ErrorResponse> handleInsufficientFunds(InsufficientFundsException ex) {
        log.warn("Insufficient funds: {}", ex.getMessage());
        return ResponseEntity.unprocessableEntity()
                .body(new ErrorResponse("insufficient_funds"));
    }

    @ExceptionHandler(InvalidTransferStateException.class)
    public ResponseEntity<ErrorResponse> handleInvalidTransferState(InvalidTransferStateException ex) {
        log.warn("Invalid state transition: {}", ex.getMessage());
        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(new ErrorResponse("transfer_already_completed"));
    }

    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<ErrorResponse> handleWalletNotFound(WalletNotFoundException ex) {
        log.warn("Wallet not found: {}", ex.getMessage());
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(new ErrorResponse("wallet_not_found"));
    }

    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<ErrorResponse> handleValidationError(MethodArgumentNotValidException ex) {
        // Field errors first (e.g. @NotBlank, @Positive)
        String field = null;
        String message = "validation_error";

        if (!ex.getBindingResult().getFieldErrors().isEmpty()) {
            FieldError fieldError = ex.getBindingResult().getFieldErrors().get(0);
            field = fieldError.getField();
            message = fieldError.getDefaultMessage();
        } else if (!ex.getBindingResult().getGlobalErrors().isEmpty()) {
            // Fall back to class-level errors (e.g. @NotSelfTransfer)
            message = ex.getBindingResult().getGlobalErrors().get(0).getDefaultMessage();
        }

        log.warn("Validation error on field [{}]: {}", field, message);
        return ResponseEntity.badRequest().body(new ErrorResponse(message));
    }

    @ExceptionHandler(Exception.class)
    public ResponseEntity<ErrorResponse> handleGeneric(Exception ex) {
        log.error("Unexpected error", ex);
        return ResponseEntity.internalServerError()
                .body(new ErrorResponse("internal_server_error"));
    }
}

