package com.viditec.wallettransfer.repository;

public interface TransferOperations {

    String createPending(String idempotencyKey, String fromWalletId, String toWalletId, long amount);

    void markProcessed(String transferId);

    void markFailed(String transferId, String failureReason);
}
