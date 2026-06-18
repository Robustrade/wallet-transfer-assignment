package com.example.wallet_transfer_assignment.exception;

import com.example.wallet_transfer_assignment.dto.ErrorResponse;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.validation.FieldError;
import org.springframework.validation.ObjectError;
import java.util.stream.Collectors;
import java.util.List;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.time.LocalDateTime;

@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(WalletNotFoundException.class)
    public ResponseEntity<ErrorResponse> handleWalletNotFound(
            WalletNotFoundException ex) {

        return ResponseEntity.status(HttpStatus.NOT_FOUND)
                .body(new ErrorResponse(
                        "WALLET_NOT_FOUND",
                        ex.getMessage(),
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(DataIntegrityViolationException.class)
    public ResponseEntity<ErrorResponse> handleDataIntegrityViolation(
            DataIntegrityViolationException ex) {

        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(new ErrorResponse(
                        "DUPLICATE_REQUEST",
                        "Duplicate idempotency request detected",
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(InsufficientBalanceException.class)
    public ResponseEntity<ErrorResponse> handleInsufficientBalance(
            InsufficientBalanceException ex) {

        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(new ErrorResponse(
                        "INSUFFICIENT_BALANCE",
                        ex.getMessage(),
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(DuplicateRequestException.class)
    public ResponseEntity<ErrorResponse> handleDuplicateRequest(
            DuplicateRequestException ex) {

        return ResponseEntity.status(HttpStatus.CONFLICT)
                .body(new ErrorResponse(
                        "DUPLICATE_REQUEST",
                        ex.getMessage(),
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(InvalidTransferException.class)
    public ResponseEntity<ErrorResponse> handleInvalidTransfer(
            InvalidTransferException ex) {

        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(new ErrorResponse(
                        "INVALID_TRANSFER",
                        ex.getMessage(),
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(MethodArgumentNotValidException.class)
    public ResponseEntity<ErrorResponse> handleValidationException(
            MethodArgumentNotValidException ex) {

        String message = "Validation failed";

        if (ex.getBindingResult() != null) {
            List<FieldError> fieldErrors = ex.getBindingResult().getFieldErrors();
            if (fieldErrors != null && !fieldErrors.isEmpty()) {
                message = fieldErrors.stream()
                        .map(fe -> {
                            String defaultMsg = fe.getDefaultMessage();
                            return fe.getField() + (defaultMsg != null ? ": " + defaultMsg : "");
                        })
                        .collect(Collectors.joining("; "));
            } else {
                List<ObjectError> globalErrors = ex.getBindingResult().getGlobalErrors();
                if (globalErrors != null && !globalErrors.isEmpty()) {
                    message = globalErrors.stream()
                            .map(ObjectError::getDefaultMessage)
                            .filter(m -> m != null && !m.isBlank())
                            .collect(Collectors.joining("; "));
                } else if (ex.getMessage() != null && !ex.getMessage().isBlank()) {
                    message = ex.getMessage();
                }
            }
        }

        return ResponseEntity.status(HttpStatus.BAD_REQUEST)
                .body(new ErrorResponse(
                        "VALIDATION_ERROR",
                        message,
                        LocalDateTime.now()
                ));
    }

    @ExceptionHandler(Exception.class)
    public ResponseEntity<ErrorResponse> handleGenericException(
            Exception ex) {

        return ResponseEntity.status(HttpStatus.INTERNAL_SERVER_ERROR)
                .body(new ErrorResponse(
                        "INTERNAL_SERVER_ERROR",
                        ex.getMessage(),
                        LocalDateTime.now()
                ));
    }
}