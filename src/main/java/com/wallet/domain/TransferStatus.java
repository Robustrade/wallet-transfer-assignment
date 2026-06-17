package com.wallet.domain;

public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED;

    public boolean canTransitionTo(TransferStatus target) {
        return switch (this) {
            case PENDING -> target == PROCESSED || target == FAILED;
            case PROCESSED, FAILED -> false;
        };
    }
}
