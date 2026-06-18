package com.wallet.transfer.repository;

import com.wallet.transfer.domain.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.UUID;

/**
 * Idempotency Record Repository
 * 
 * Data access layer for IdempotencyRecord entities.
 * 
 * Key Methods:
 * - findByIdempotencyKey: Find cached response for duplicate requests
 */
@Repository
public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, UUID> {
    
    /**
     * Find idempotency record by idempotency key
     * 
     * Used to detect duplicate requests and return cached response.
     * UNIQUE constraint ensures at most 1 record per key.
     * 
     * @param idempotencyKey the idempotency key
     * @return Optional containing cached response if found
     */
    Optional<IdempotencyRecord> findByIdempotencyKey(String idempotencyKey);
}
