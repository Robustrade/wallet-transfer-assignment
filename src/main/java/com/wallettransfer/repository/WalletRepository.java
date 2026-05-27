package com.wallettransfer.repository;

import com.wallettransfer.domain.Wallet;
import jakarta.persistence.LockModeType;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

/**
 * Persistence access for {@link Wallet} entities.
 */
public interface WalletRepository extends JpaRepository<Wallet, String> {

    /**
     * Loads a wallet with a pessimistic write lock for safe balance updates under concurrency.
     *
     * @param id wallet id
     * @return locked wallet if found
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM Wallet w WHERE w.id = :id")
    Optional<Wallet> findByIdForUpdate(@Param("id") String id);
}
