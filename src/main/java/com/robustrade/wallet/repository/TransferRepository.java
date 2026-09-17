package com.robustrade.wallet.repository;

import com.robustrade.wallet.entities.Transfer;
import java.math.BigDecimal;
import java.util.Optional;
import java.util.UUID;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Modifying;
import org.springframework.data.jpa.repository.Query;
import org.springframework.data.repository.query.Param;

public interface TransferRepository extends JpaRepository<Transfer, UUID> {

  Optional<Transfer> findByIdempotencyKey(String idempotencyKey);

  /**
   * Inserts a PENDING transfer, relying on the UNIQUE constraint on idempotency_key to make
   * check-and-insert one atomic step.
   *
   * @return 1 when the row was inserted, 0 when the key already exists (duplicate request)
   */
  @Modifying
  @Query(
      value =
          """
          INSERT INTO transfers (
              id, idempotency_key, from_wallet_id, to_wallet_id,
              initiated_by, amount, status, created_at, updated_at)
          VALUES (:id, :idempotencyKey, :fromWalletId, :toWalletId,
              :initiatedBy, :amount, 'PENDING', now(), now())
          ON CONFLICT (idempotency_key) DO NOTHING
          """,
      nativeQuery = true)
  int insertIfAbsent(
      @Param("id") UUID id,
      @Param("idempotencyKey") String idempotencyKey,
      @Param("fromWalletId") String fromWalletId,
      @Param("toWalletId") String toWalletId,
      @Param("initiatedBy") UUID initiatedBy,
      @Param("amount") BigDecimal amount);
}
