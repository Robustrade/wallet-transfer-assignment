package com.viditec.wallettransfer.repository;

public record IdempotencyRecord(
        String idempotencyKey,
        String requestHash,
        String transferId,
        Integer httpStatus,
        String responseBody
) {
}
