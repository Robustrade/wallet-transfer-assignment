package com.example.wallettransfer.repository;

import com.example.wallettransfer.domain.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.Optional;

public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, String> {

    @Query("select r from IdempotencyRecord r join fetch r.transfer where r.idempotencyKey = :key")
    Optional<IdempotencyRecord> findWithTransferByKey(@Param("key") String key);
}
