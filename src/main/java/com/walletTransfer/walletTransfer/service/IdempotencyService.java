package com.walletTransfer.walletTransfer.service;

import com.walletTransfer.walletTransfer.dto.TransferResponse;
import com.walletTransfer.walletTransfer.entity.IdempotencyRecord;
import com.walletTransfer.walletTransfer.entity.Transfer;

import java.util.Optional;

public interface IdempotencyService {
    Optional<TransferResponse> findExistingResponse(String idempotencyKey, String requestHash);

    IdempotencyRecord createPendingRecord(String idempotencyKey, String requestHash);

    void markCompleted(IdempotencyRecord record, Transfer transfer, TransferResponse response);

    void markFailed(IdempotencyRecord record);
}
