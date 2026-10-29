package com.example.wallet.handler;

import com.example.wallet.domain.IdempotencyKeyReusedException;
import com.example.wallet.domain.IllegalTransferStateException;
import com.example.wallet.domain.InvalidTransferException;
import com.example.wallet.domain.TransferNotFoundException;
import com.example.wallet.domain.WalletAlreadyExistsException;
import com.example.wallet.domain.WalletNotFoundException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.method.annotation.MethodArgumentTypeMismatchException;

@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(MethodArgumentNotValidException.class)
    ResponseEntity<ErrorResponse> validation(MethodArgumentNotValidException e) {
        String msg = e.getBindingResult().getFieldErrors().stream()
                .map(f -> f.getField() + " " + f.getDefaultMessage())
                .sorted()
                .reduce((a, b) -> a + "; " + b)
                .orElse("invalid request");
        return build(HttpStatus.BAD_REQUEST, "VALIDATION_ERROR", msg);
    }

    @ExceptionHandler({
        HttpMessageNotReadableException.class,
        MethodArgumentTypeMismatchException.class,
        InvalidTransferException.class
    })
    ResponseEntity<ErrorResponse> badRequest(Exception e) {
        return build(HttpStatus.BAD_REQUEST, "BAD_REQUEST", e.getMessage());
    }

    @ExceptionHandler({WalletNotFoundException.class, TransferNotFoundException.class})
    ResponseEntity<ErrorResponse> notFound(RuntimeException e) {
        return build(HttpStatus.NOT_FOUND, "NOT_FOUND", e.getMessage());
    }

    @ExceptionHandler(WalletAlreadyExistsException.class)
    ResponseEntity<ErrorResponse> exists(WalletAlreadyExistsException e) {
        return build(HttpStatus.CONFLICT, "WALLET_EXISTS", e.getMessage());
    }

    @ExceptionHandler(IdempotencyKeyReusedException.class)
    ResponseEntity<ErrorResponse> keyReused(IdempotencyKeyReusedException e) {
        return build(HttpStatus.UNPROCESSABLE_ENTITY, "IDEMPOTENCY_KEY_REUSED", e.getMessage());
    }

    @ExceptionHandler(IllegalTransferStateException.class)
    ResponseEntity<ErrorResponse> illegalState(IllegalTransferStateException e) {
        return build(HttpStatus.CONFLICT, "ILLEGAL_STATE", e.getMessage());
    }

    private static ResponseEntity<ErrorResponse> build(HttpStatus status, String code, String message) {
        return ResponseEntity.status(status).body(new ErrorResponse(code, message));
    }
}
