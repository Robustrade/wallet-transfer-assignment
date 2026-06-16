package com.wallet.domain;

/**
 * Terminal outcome of a synchronous transfer. Rows are inserted directly in
 * one of these states — there is no intermediate PENDING phase to observe.
 */
public enum TransferStatus {
    PROCESSED,
    FAILED
}
