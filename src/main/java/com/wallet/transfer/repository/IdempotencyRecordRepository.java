package com.wallet.transfer.repository;

import com.wallet.transfer.model.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

/**
 * Repository interface for managing {@link IdempotencyRecord} entity persistence.
 * Used to save and retrieve response payloads associated with unique idempotency keys
 * to guarantee API request safety and prevent duplicate transaction executions.
 */
@Repository
public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, String> {
}
