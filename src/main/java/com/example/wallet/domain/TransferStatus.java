package com.example.wallet.domain;

import java.util.EnumSet;
import java.util.Set;

/** Transfer lifecycle: PENDING -> PROCESSED or PENDING -> FAILED. Both end states are terminal. */
public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED;

    public Set<TransferStatus> allowedNext() {
        return this == PENDING ? EnumSet.of(PROCESSED, FAILED) : EnumSet.noneOf(TransferStatus.class);
    }

    public boolean canTransitionTo(TransferStatus next) {
        return allowedNext().contains(next);
    }

    public boolean isTerminal() {
        return this != PENDING;
    }
}
