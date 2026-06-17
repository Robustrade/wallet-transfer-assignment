package com.robustrade.wallet_transfer_service.exception;

import org.springframework.http.HttpStatus;
import org.springframework.web.bind.annotation.*;

import java.util.Map;

@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(InsufficientBalanceException.class)
    @ResponseStatus(HttpStatus.BAD_REQUEST)
    public Map<String, String> handleInsufficientBalance(
            InsufficientBalanceException ex) {

        return Map.of(
                "error", ex.getMessage()
        );
    }
}