package com.wallettransfer.idempotency.repository;

import com.wallettransfer.idempotency.domain.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;

public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, String> {
}
