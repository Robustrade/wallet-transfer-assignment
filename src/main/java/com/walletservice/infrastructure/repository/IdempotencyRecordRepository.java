package com.walletservice.infrastructure.repository;

import com.walletservice.domain.entity.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.stereotype.Repository;

/**
 * Repository for IdempotencyRecord persistence operations.
 *
 * Design notes:
 * - idempotencyKey is the primary key, ensuring exactly one record per key
 * - Duplicate inserts will fail at the database level, providing strong consistency
 */
@Repository
public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, String> {
}

