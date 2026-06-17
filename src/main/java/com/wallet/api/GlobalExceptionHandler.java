package com.wallet.api;

import com.wallet.api.dto.ErrorResponse;
import com.wallet.api.exception.IdempotencyConflictException;
import com.wallet.api.exception.InvalidTransferException;
import com.wallet.api.exception.TransferNotFoundException;
import com.wallet.api.exception.WalletNotFoundException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<ErrorResponse> handleWalletNotFound(WalletNotFoundException ex) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(new ErrorResponse("wallet_not_found", ex.getMessage()));
    }

    @ExceptionHandler(TransferNotFoundException.class)
    public ResponseEntity<ErrorResponse> handleTransferNotFound(TransferNotFoundException ex) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(new ErrorResponse("transfer_not_found", ex.getMessage()));
    }

    @ExceptionHandler(IdempotencyConflictException.class)
    public ResponseEntity<ErrorResponse> handleIdempotencyConflict(
            IdempotencyConflictException ex) {
        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(new ErrorResponse("idempotency_conflict", ex.getMessage()));
    }

    @ExceptionHandler(InvalidTransferException.class)
    public ResponseEntity<ErrorResponse> handleInvalidTransfer(InvalidTransferException ex) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(new ErrorResponse("invalid_transfer", ex.getMessage()));
    }

    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<ErrorResponse> handleValidation(MethodArgumentNotValidException ex) {
        String message =
                ex.getBindingResult().getFieldErrors().stream()
                        .map(error -> error.getField() + ": " + error.getDefaultMessage())
                        .findFirst()
                        .orElse("Validation failed");
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(new ErrorResponse("validation_error", message));
    }
}
