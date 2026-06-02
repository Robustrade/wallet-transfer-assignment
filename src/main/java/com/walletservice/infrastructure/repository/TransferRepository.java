package com.walletservice.infrastructure.repository;

import com.walletservice.domain.entity.Transfer;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import jakarta.persistence.LockModeType;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for Transfer persistence operations.
 *
 * Design notes:
 * - findByIdForUpdate uses pessimistic locking for safe state transitions
 * - Ensures only one thread can transition a transfer's status at a time
 */
@Repository
public interface TransferRepository extends JpaRepository<Transfer, String> {
    /**
     * Retrieves a transfer with a pessimistic write lock for safe state transitions.
     * The lock prevents concurrent status modifications.
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT t FROM Transfer t WHERE t.id = :id")
    Optional<Transfer> findByIdForUpdate(@Param("id") String id);
}

