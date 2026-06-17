package com.wallet.transfer.repository;

import com.wallet.transfer.model.Wallet;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

import java.util.Optional;

/**
 * Repository interface for managing {@link Wallet} persistence.
 * Provides custom query methods for row-level database locking (SELECT FOR UPDATE)
 * to ensure safe, thread-safe updates to wallet balances.
 */
public interface WalletRepository extends JpaRepository<Wallet, String> {

    /**
     * Retrieves a wallet by its ID and acquires a pessimistic write lock (SELECT FOR UPDATE) on the row.
     * This prevents other concurrent transactions from reading or updating the same wallet's balance
     * until the current transaction commits or rolls back, ensuring strict transactional consistency
     * and avoiding race conditions or double-spending.
     *
     * @param id The wallet identifier.
     * @return An Optional containing the locked Wallet, or empty if not found.
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM Wallet w WHERE w.id = :id")
    Optional<Wallet> findByIdForUpdate(@Param("id") String id);

}
