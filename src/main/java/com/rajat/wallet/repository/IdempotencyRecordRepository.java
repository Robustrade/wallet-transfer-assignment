package com.rajat.wallet.repository;

import com.rajat.wallet.domain.entities.IdempotencyRecord;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;

public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, UUID> {

  Optional<IdempotencyRecord> findByIdempotencyKey(String idempotencyKey);
}
