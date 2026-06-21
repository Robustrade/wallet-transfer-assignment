package com.rajat.wallet.repository;

import com.rajat.wallet.domain.entities.Wallet;
import jakarta.persistence.LockModeType;
import java.util.Collection;
import java.util.List;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Lock;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface WalletRepository extends JpaRepository<Wallet, UUID> {

  /**
   * Loads the given wallets under a pessimistic write lock ({@code SELECT … FOR UPDATE}), ordered
   * by id. The deterministic order is the deadlock-avoidance strategy: two opposing transfers
   * between the same pair always acquire the row locks in the same sequence.
   */
  @Lock(LockModeType.PESSIMISTIC_WRITE)
  @Query("select w from Wallet w where w.id in :ids order by w.id")
  List<Wallet> findAllForUpdate(@Param("ids") Collection<UUID> ids);
}
