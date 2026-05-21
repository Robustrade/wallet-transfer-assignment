package com.kullu.wallet.controller;

import java.util.stream.Collectors;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import com.kullu.wallet.dto.response.ErrorResponse;
import com.kullu.wallet.exception.IdempotencyConflictException;
import com.kullu.wallet.exception.SelfTransferException;
import com.kullu.wallet.exception.WalletNotFoundException;

@RestControllerAdvice
public class GlobalExceptionHandler {

    private static final Logger log = LoggerFactory.getLogger(GlobalExceptionHandler.class);

    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<ErrorResponse> onValidation(MethodArgumentNotValidException ex) {
        String details = ex.getBindingResult().getFieldErrors().stream()
            .map(fe -> fe.getField() + ": " + fe.getDefaultMessage())
            .collect(Collectors.joining(", "));
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
            .body(new ErrorResponse("VALIDATION_ERROR", details));
    }

    @ExceptionHandler(HttpMessageNotReadableException.class)
    public ResponseEntity<ErrorResponse> onUnreadable(HttpMessageNotReadableException ex) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
            .body(new ErrorResponse("MALFORMED_REQUEST", "request body is not valid JSON"));
    }

    @ExceptionHandler(SelfTransferException.class)
    public ResponseEntity<ErrorResponse> onSelfTransfer(SelfTransferException ex) {
        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
            .body(new ErrorResponse("SELF_TRANSFER", ex.getMessage()));
    }

    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<ErrorResponse> onWalletNotFound(WalletNotFoundException ex) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND)
            .body(new ErrorResponse("WALLET_NOT_FOUND", ex.getMessage()));
    }

    @ExceptionHandler(IdempotencyConflictException.class)
    public ResponseEntity<ErrorResponse> onIdempotencyConflict(IdempotencyConflictException ex) {
        String code = switch (ex.getKind()) {
            case PAYLOAD_MISMATCH -> "IDEMPOTENCY_CONFLICT";
            case IN_FLIGHT -> "IDEMPOTENCY_IN_FLIGHT";
        };
        return ResponseEntity.status(HttpStatus.CONFLICT)
            .body(new ErrorResponse(code, ex.getMessage()));
    }

    @ExceptionHandler(Exception.class)
    public ResponseEntity<ErrorResponse> onUnexpected(Exception ex) {
        log.error("unhandled exception", ex);
        return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
            .body(new ErrorResponse("INTERNAL_ERROR", "an unexpected error occurred"));
    }
}
