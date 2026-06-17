package com.wallet.transfer.model;

/**
 * Enum defining the possible execution states of a wallet-to-wallet transfer transaction.
 */
public enum TransferStatus {
    /**
     * The transfer transaction has been created and is waiting to be processed.
     */
    PENDING,

    /**
     * The transfer has completed successfully (funds deducted, credited, and ledger entries recorded).
     */
    PROCESSED,

    /**
     * The transfer failed (e.g. due to insufficient funds in the source wallet).
     */
    FAILED
}
