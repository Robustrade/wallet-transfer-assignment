package com.mycompany.wallet_transaction.repository;

import com.mycompany.wallet_transaction.entities.IdempotencyRecord;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Modifying;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.Optional;

public interface IdempotencyRecordRepository extends JpaRepository<IdempotencyRecord, Long> {
    Optional<IdempotencyRecord> findByIdempotencyKey(String idempotencyKey);

    @Modifying
    @Query(value = """
            insert into idempotency_record (idempotency_key,status,result_status, result_message)
            values (:idempotencyKey, 'IN_PROGRESS', null, null)
            on conflict (idempotency_key) do nothing
            """, nativeQuery = true)
    int claimIdempotencyKey(@Param("idempotencyKey") String idempotencyKey);
}
