package com.wallet.domain;

import java.util.Set;

public enum TransferStatus {
    PENDING,
    PROCESSED,
    FAILED;

    private static final Set<TransferStatus> TERMINAL = Set.of(PROCESSED, FAILED);

    public boolean isTerminal() {
        return TERMINAL.contains(this);
    }

    public boolean canTransitionTo(TransferStatus next) {
        if (this == PENDING) {
            return next == PROCESSED || next == FAILED;
        }
        return false;
    }
}
