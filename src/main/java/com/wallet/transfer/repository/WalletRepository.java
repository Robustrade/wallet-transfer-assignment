package com.wallet.transfer.repository;

import com.wallet.transfer.domain.Wallet;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.UUID;

/**
 * Wallet Repository
 * 
 * Data access layer for Wallet entities.
 * 
 * Key Methods:
 * - findByWalletId: Find wallet by business key
 * - findByWalletIdForUpdate: Find wallet with pessimistic lock (FOR UPDATE)
 */
@Repository
public interface WalletRepository extends JpaRepository<Wallet, UUID> {
    
    /**
     * Find wallet by business key (walletId)
     * @param walletId the wallet ID
     * @return Optional containing wallet if found
     */
    Optional<Wallet> findByWalletId(String walletId);
    
    /**
     * Find wallet by walletId with pessimistic write lock
     * 
     * Acquires row-level lock (SELECT ... FOR UPDATE) to prevent concurrent modifications.
     * Used during transfer execution to ensure safe balance updates.
     * 
     * @param walletId the wallet ID
     * @return Optional containing locked wallet if found
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM Wallet w WHERE w.walletId = :walletId")
    Optional<Wallet> findByWalletIdForUpdate(@Param("walletId") String walletId);
}
