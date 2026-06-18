package com.example.wallet_transfer_assignment.repository;


import com.example.wallet_transfer_assignment.entity.Wallet;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

import java.util.Optional;
import java.util.UUID;

public interface WalletRepository extends JpaRepository<Wallet, UUID> {


    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("""
            SELECT w
            FROM Wallet w
            WHERE w.id = :walletId
            """)
    Optional<Wallet> findByIdForUpdate(@Param("walletId")UUID walletId);
}