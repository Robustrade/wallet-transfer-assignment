package com.wallettransfer.wallet.repository;

import com.wallettransfer.wallet.domain.Wallet;
import jakarta.persistence.LockModeType;
import java.util.List;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface WalletRepository extends JpaRepository<Wallet, String> {

    @Lock(LockModeType.PESSIMISTIC_WRITE)
    @Query("SELECT w FROM Wallet w WHERE w.id IN :ids ORDER BY w.id")
    List<Wallet> findByIdsForUpdate(@Param("ids") List<String> ids);
}
