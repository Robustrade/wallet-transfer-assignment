package com.wallettransfer.service;

import com.wallettransfer.domain.Transfer;

/**
 * Outcome of a transfer request. The transfer is PROCESSED or FAILED.
 *
 * @param replayed {@code true} when this is the stored result of an earlier request with the same
 *     idempotency key
 */
public record TransferResult(Transfer transfer, boolean replayed) {}
