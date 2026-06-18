package com.wallet.domain;

public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED;

    public boolean canTransitionTo(TransferStatus next) {
        return this == PENDING && (next == PROCESSED || next == FAILED);
    }
}
