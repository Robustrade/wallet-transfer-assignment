package com.viditec.wallettransfer.repository;

import java.util.Optional;

public interface IdempotencyOperations {

    boolean reserve(String idempotencyKey, String requestHash);

    Optional<IdempotencyRecord> findByKey(String idempotencyKey);

    void complete(String idempotencyKey, String transferId, int httpStatus, String responseBody);
}
