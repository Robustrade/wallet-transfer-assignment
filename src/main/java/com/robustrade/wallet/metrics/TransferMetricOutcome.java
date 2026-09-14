package com.robustrade.wallet.metrics;

/**
 * Business outcomes recorded for transfer processing metrics.
 */
public enum TransferMetricOutcome {
    NEW_PROCESSED,
    REPLAY_PROCESSED,
    RETRY_PROCESSED,
    RESUME_PROCESSED,
    INSUFFICIENT_BALANCE,
    IDEMPOTENCY_CONFLICT,
    WALLETS_NOT_FOUND,
    TRANSFER_FAILED,
    UNEXPECTED
}
