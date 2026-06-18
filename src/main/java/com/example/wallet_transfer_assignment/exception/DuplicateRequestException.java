package com.example.wallet_transfer_assignment.exception;

public class DuplicateRequestException extends RuntimeException {

    public DuplicateRequestException(String message) {
        super(message);
    }
}