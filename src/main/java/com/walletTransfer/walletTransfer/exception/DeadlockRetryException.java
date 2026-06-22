package com.walletTransfer.walletTransfer.exception;

import org.springframework.dao.CannotAcquireLockException;

public class DeadlockRetryException extends RuntimeException {
    public DeadlockRetryException(String message, CannotAcquireLockException ex) {
        super(message);
    }
}
