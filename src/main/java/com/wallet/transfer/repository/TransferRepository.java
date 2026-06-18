package com.wallet.transfer.repository;

import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferStatus;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.UUID;

/**
 * Transfer Repository
 * 
 * Data access layer for Transfer entities.
 * 
 * Key Methods:
 * - findByTransferId: Find transfer by business key
 * - findByIdempotencyKey: Find transfer by idempotency key (for deduplication)
 */
@Repository
public interface TransferRepository extends JpaRepository<Transfer, UUID> {
    
    /**
     * Find transfer by business key (transferId)
     * @param transferId the transfer ID
     * @return Optional containing transfer if found
     */
    Optional<Transfer> findByTransferId(String transferId);
    
    /**
     * Find transfer by idempotency key
     * 
     * Used for deduplication: if the same idempotencyKey is used again,
     * this query finds the original transfer to return as cached result.
     * 
     * @param idempotencyKey the idempotency key
     * @return Optional containing transfer if found
     */
    Optional<Transfer> findByIdempotencyKey(String idempotencyKey);
}
