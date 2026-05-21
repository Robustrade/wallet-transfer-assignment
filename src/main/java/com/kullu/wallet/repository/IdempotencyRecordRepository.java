package com.kullu.wallet.repository;

import java.util.Optional;
import java.util.UUID;

import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Modifying;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import com.kullu.wallet.entity.IdempotencyEntity;

public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyEntity, String> {

    Optional<IdempotencyEntity> findByKey(String key);

    @Modifying
    @Query(value = """
        INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at, updated_at)
        VALUES (:key, :hash, :transferId, NOW(), NOW())
        ON CONFLICT (key) DO NOTHING
        """, nativeQuery = true)
    int tryClaim(@Param("key") String key,
                 @Param("hash") String hash,
                 @Param("transferId") UUID transferId);
}
