package com.wallettransfer.domain;

/**
 * Lifecycle states for a transfer.
 * Only PENDING may move to PROCESSED or FAILED; terminal states do not change.
 */
public enum TransferStatus {
    /** Transfer created but not yet applied to balances. */
    PENDING,
    /** Funds moved and ledger entries written. */
    PROCESSED,
    /** Transfer could not complete (e.g. insufficient funds). */
    FAILED;

    /**
     * Returns whether transitioning from this state to {@code next} is allowed.
     *
     * @param next target status
     * @return true if the transition is valid (including no-op same state)
     */
    public boolean canTransitionTo(TransferStatus next) {
        if (this == next) {
            return true;
        }
        return this == PENDING && (next == PROCESSED || next == FAILED);
    }
}
