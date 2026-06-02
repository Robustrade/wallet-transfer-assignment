package com.walletservice.infrastructure.repository;

import com.walletservice.domain.entity.Wallet;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import jakarta.persistence.LockModeType;
import java.util.Optional;
import java.util.UUID;

/**
 * Repository for Wallet persistence operations.
 *
 * Design notes:
 * - findByIdForUpdate uses pessimistic locking to prevent race conditions during transfers
 * - Lock is acquired at database level before reading the wallet state
 * - This ensures only one transaction can modify a wallet at a time
 */
@Repository
public interface WalletRepository extends JpaRepository<Wallet, String> {
    /**
     * Retrieves a wallet with a pessimistic write lock.
     * Used before updating wallet balance to prevent concurrent modifications.
     * The lock is held until the transaction commits.
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM Wallet w WHERE w.id = :id")
    Optional<Wallet> findByIdForUpdate(@Param("id") String id);
}

