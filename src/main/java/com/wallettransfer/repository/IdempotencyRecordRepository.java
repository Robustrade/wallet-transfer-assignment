package com.wallettransfer.repository;

import com.wallettransfer.domain.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;

/**
 * Persistence access for {@link IdempotencyRecord} rows (primary key = idempotency key).
 */
public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, String> {}
