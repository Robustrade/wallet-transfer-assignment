package com.example.wallet.exception;

import com.example.wallet.model.TransferStatus;

import java.util.UUID;

/**
 * Thrown when a state transition is attempted on a transfer that is no longer PENDING.
 * Protects terminal states (PROCESSED, FAILED) from being overwritten by concurrent retries.
 */
public class InvalidTransferStateException extends RuntimeException {

    public InvalidTransferStateException(UUID transferId, TransferStatus attemptedStatus) {
        super("Cannot transition transfer " + transferId + " to " + attemptedStatus
                + ": transfer is no longer PENDING (already in a terminal state)");
    }
}

