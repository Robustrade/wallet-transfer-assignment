package com.wallettransfer.api;

import com.wallettransfer.service.InsufficientFundsException;
import com.wallettransfer.service.InvalidTransferException;
import com.wallettransfer.service.WalletNotFoundException;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;

import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

@RestControllerAdvice
public class ApiExceptionHandler {
    @ExceptionHandler({InvalidTransferException.class, MethodArgumentNotValidException.class})
    ResponseEntity<Map<String, String>> badRequest(Exception exception) {
        String message = exception instanceof MethodArgumentNotValidException validationException
                ? validationMessage(validationException)
                : exception.getMessage();
        return ResponseEntity.badRequest().body(Map.of("error", message == null ? "Invalid request" : message));
    }

    @ExceptionHandler(InsufficientFundsException.class)
    ResponseEntity<Map<String, String>> insufficientFunds(InsufficientFundsException exception) {
        return ResponseEntity.status(HttpStatus.CONFLICT).body(Map.of("error", exception.getMessage()));
    }

    @ExceptionHandler(WalletNotFoundException.class)
    ResponseEntity<Map<String, String>> walletNotFound(WalletNotFoundException exception) {
        return ResponseEntity.status(HttpStatus.NOT_FOUND).body(Map.of("error", exception.getMessage()));
    }

    private String validationMessage(MethodArgumentNotValidException exception) {
        List<String> errors = exception.getBindingResult().getFieldErrors().stream()
                .map(error -> error.getField() + ": " + error.getDefaultMessage())
                .collect(Collectors.toList());
        return String.join(", ", errors);
    }
}