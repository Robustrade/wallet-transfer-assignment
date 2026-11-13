package com.wallettransfer.repository.jdbc;

import com.wallettransfer.domain.IdempotencyRecord;
import com.wallettransfer.repository.IdempotencyRepository;
import java.util.Optional;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** PostgreSQL implementation of {@link IdempotencyRepository}. */
@Repository
class JdbcIdempotencyRepository implements IdempotencyRepository {

  private final JdbcClient jdbc;

  JdbcIdempotencyRepository(JdbcClient jdbc) {
    this.jdbc = jdbc;
  }

  @Override
  public boolean tryClaim(IdempotencyRecord record) {
    int inserted =
        jdbc.sql(
                """
                INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at)
                VALUES (:key, :hash, :transferId, :createdAt)
                ON CONFLICT (key) DO NOTHING
                """)
            .param("key", record.key())
            .param("hash", record.requestHash())
            .param("transferId", record.transferId())
            .param("createdAt", Timestamps.toDb(record.createdAt()))
            .update();
    return inserted == 1;
  }

  @Override
  public Optional<IdempotencyRecord> findByKey(String key) {
    return jdbc.sql(
            "SELECT key, request_hash, transfer_id, created_at FROM idempotency_records"
                + " WHERE key = :key")
        .param("key", key)
        .query(
            (rs, rowNum) ->
                new IdempotencyRecord(
                    rs.getString("key"),
                    rs.getString("request_hash"),
                    rs.getString("transfer_id"),
                    Timestamps.read(rs, "created_at")))
        .optional();
  }
}
