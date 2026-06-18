package com.wallet.service;

import com.wallet.domain.Transfer;

import lombok.Getter;

/**
 * Outcome of a transfer request. {@code replay} distinguishes a freshly executed transfer from
 * one returned because the idempotency key had already been processed, so the handler layer can
 * pick the right HTTP status (201 vs 200) without re-deriving it from timestamps.
 */
@Getter
public class TransferResult {

    private final Transfer transfer;
    private final boolean replay;

    public TransferResult(Transfer transfer, boolean replay) {
        this.transfer = transfer;
        this.replay = replay;
    }
}
