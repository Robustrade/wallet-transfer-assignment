package com.example.wallet.model;

/**
 * Allowed states for a transfer.
 *
 * State machine:
 *   PENDING -> PROCESSED
 *   PENDING -> FAILED
 *
 * Both PROCESSED and FAILED are terminal states.
 */
public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED
}

