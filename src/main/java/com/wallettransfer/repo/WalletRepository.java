package com.wallettransfer.repo;

import com.wallettransfer.entity.Wallet;
import jakarta.persistence.LockModeType;
import java.util.List;
import java.util.Optional;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;
import org.springframework.stereotype.Repository;

@Repository
public interface WalletRepository extends JpaRepository<Wallet, String> {

  @Lock(LockModeType.PESSIMISTIC_WRITE)
  @Query("SELECT w FROM Wallet w WHERE w.walletId = :walletId")
  Optional<Wallet> findByWalletIdForUpdate(@Param("walletId") String walletId);

  @Lock(LockModeType.PESSIMISTIC_WRITE)
  @Query("SELECT w FROM Wallet w WHERE w.walletId IN :walletIds ORDER BY w.walletId ASC")
  List<Wallet> findByWalletIdsForUpdate(@Param("walletIds") List<String> walletIds);
}
