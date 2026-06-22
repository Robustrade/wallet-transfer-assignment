package com.walletTransfer.walletTransfer.repository;

import com.walletTransfer.walletTransfer.entity.Wallet;
import jakarta.persistence.LockModeType;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;

import java.util.List;
import java.util.UUID;
public interface WalletRepository extends JpaRepository<Wallet, UUID> {


    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("""
            SELECT w
            FROM Wallet w
            WHERE w.id IN :walletIds
            ORDER BY w.id
           """)
    List<Wallet> findAndLockWallets(List<UUID> walletIds);
}
