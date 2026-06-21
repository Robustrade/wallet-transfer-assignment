package com.rajat.wallet.repository;

import com.rajat.wallet.domain.entities.Transfer;
import java.util.UUID;
import org.springframework.data.domain.Page;
import org.springframework.data.domain.Pageable;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface TransferRepository extends JpaRepository<Transfer, UUID> {

  /**
   * Paginated transfer history for a wallet — every transfer it took part in as source or
   * destination. Served by the FK indexes on {@code from_wallet_id} / {@code to_wallet_id};
   * ordering is supplied by the {@link Pageable}.
   */
  @Query(
      value =
          "select t from Transfer t where t.fromWalletId = :walletId or t.toWalletId = :walletId",
      countQuery =
          "select count(t) from Transfer t where t.fromWalletId = :walletId"
              + " or t.toWalletId = :walletId")
  Page<Transfer> findByWalletId(@Param("walletId") UUID walletId, Pageable pageable);
}
