package com.example.wallet.exception;

public class IdempotencyConflictException extends BusinessException {

    public IdempotencyConflictException(String message) {
        super(message);
    }
}