package com.kullu.wallet.repository;

import java.util.Optional;

import jakarta.persistence.LockModeType;

import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import com.kullu.wallet.entity.WalletEntity;

public interface WalletRepository extends JpaRepository<WalletEntity, String> {

    /**
     * Acquire a {@code FOR UPDATE} exclusive row lock on the wallet.
     *
     * <p>Hibernate's {@link LockModeType#PESSIMISTIC_WRITE} on the Postgres
     * dialect translates to {@code SELECT ... FOR UPDATE}, the strongest
     * row-level lock. This serializes concurrent debits safely.
     *
     * <p>Wallets are always locked in ascending id order by the service layer,
     * so reverse-direction transfers acquire the same two locks in the same
     * order, preventing deadlocks.
     */
    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM WalletEntity w WHERE w.id = :id")
    Optional<WalletEntity> lockById(@Param("id") String id);
}
