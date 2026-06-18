package com.wallet.transfer.domain;

/**
 * Transfer Status Enumeration
 * 
 * Defines the lifecycle states of a wallet transfer.
 * 
 * States:
 * - PENDING: Initial state, transfer created but not yet processed
 * - PROCESSED: Successfully executed (balances updated, ledger entries created)
 * - FAILED: Execution failed (insufficient balance, internal error)
 */
public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED
}
