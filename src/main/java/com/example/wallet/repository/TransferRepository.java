package com.example.wallet.repository;

import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.model.Transfer;
import com.example.wallet.model.TransferStatus;

import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

public interface TransferRepository {

    /** Find a transfer by its idempotency key. */
    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);

    /** Find a transfer by its ID. */
    Optional<Transfer> findById(UUID id);

    /**
     * Insert a new transfer in PENDING status.
     * Must handle concurrent inserts with the same idempotency key gracefully
     * (e.g. via ON CONFLICT DO NOTHING + re-read).
     */
    Transfer insertPending(CreateTransferRequest request);

    /**
     * Find all transfers that have been stuck in PENDING status since before the given threshold.
     * Used by the cleanup scheduler to detect orphaned transfers caused by transient failures.
     */
    List<Transfer> findStuckPending(Instant stuckBefore);

    /**
     * Update the status of an existing transfer.
     * Only valid transitions: PENDING -> PROCESSED, PENDING -> FAILED.
     */
    void updateStatus(UUID transferId, TransferStatus status);
}
