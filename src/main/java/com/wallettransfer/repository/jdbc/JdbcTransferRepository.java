package com.wallettransfer.repository.jdbc;

import com.wallettransfer.domain.FailureReason;
import com.wallettransfer.domain.InvalidStateTransitionException;
import com.wallettransfer.domain.Transfer;
import com.wallettransfer.domain.TransferState;
import com.wallettransfer.repository.TransferRepository;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** PostgreSQL implementation of {@link TransferRepository}. */
@Repository
class JdbcTransferRepository implements TransferRepository {

  private final JdbcClient jdbc;

  JdbcTransferRepository(JdbcClient jdbc) {
    this.jdbc = jdbc;
  }

  @Override
  public void insert(Transfer transfer) {
    jdbc.sql(
            """
            INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount,
                                   state, failure_reason, created_at, updated_at)
            VALUES (:id, :key, :from, :to, :amount, :state, :reason, :createdAt, :updatedAt)
            """)
        .param("id", transfer.id())
        .param("key", transfer.idempotencyKey())
        .param("from", transfer.fromWalletId())
        .param("to", transfer.toWalletId())
        .param("amount", transfer.amount())
        .param("state", transfer.state().name())
        .param("reason", reasonName(transfer))
        .param("createdAt", Timestamps.toDb(transfer.createdAt()))
        .param("updatedAt", Timestamps.toDb(transfer.updatedAt()))
        .update();
  }

  @Override
  public Optional<Transfer> findById(String id) {
    return jdbc.sql(
            """
            SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state,
                   failure_reason, created_at, updated_at
              FROM transfers WHERE id = :id
            """)
        .param("id", id)
        .query(
            (rs, rowNum) -> {
              String reason = rs.getString("failure_reason");
              return new Transfer(
                  rs.getString("id"),
                  rs.getString("idempotency_key"),
                  rs.getString("from_wallet_id"),
                  rs.getString("to_wallet_id"),
                  rs.getLong("amount"),
                  TransferState.valueOf(rs.getString("state")),
                  reason == null ? null : FailureReason.valueOf(reason),
                  Timestamps.read(rs, "created_at"),
                  Timestamps.read(rs, "updated_at"));
            })
        .optional();
  }

  @Override
  public void updateState(Transfer transfer) {
    int updated =
        jdbc.sql(
                """
                UPDATE transfers SET state = :state, failure_reason = :reason, updated_at = :now
                 WHERE id = :id AND state = 'PENDING'
                """)
            .param("state", transfer.state().name())
            .param("reason", reasonName(transfer))
            .param("now", Timestamps.toDb(transfer.updatedAt()))
            .param("id", transfer.id())
            .update();
    if (updated == 0) {
      throw new InvalidStateTransitionException(TransferState.PENDING, transfer.state());
    }
  }

  private static String reasonName(Transfer transfer) {
    return transfer.failureReason() == null ? null : transfer.failureReason().name();
  }
}
