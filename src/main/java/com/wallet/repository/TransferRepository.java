package com.wallet.repository;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class TransferRepository {

    private final NamedParameterJdbcTemplate jdbc;

    public TransferRepository(NamedParameterJdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(Transfer transfer) {
        var params =
                new MapSqlParameterSource()
                        .addValue("id", transfer.id())
                        .addValue("idempotencyKey", transfer.idempotencyKey())
                        .addValue("fromWalletId", transfer.fromWalletId())
                        .addValue("toWalletId", transfer.toWalletId())
                        .addValue("amount", transfer.amount())
                        .addValue("status", transfer.status().name())
                        .addValue("failureReason", transfer.failureReason())
                        .addValue("createdAt", Timestamp.from(transfer.createdAt()))
                        .addValue("updatedAt", Timestamp.from(transfer.updatedAt()));

        jdbc.update(
                """
                INSERT INTO transfers (
                    id, idempotency_key, from_wallet_id, to_wallet_id,
                    amount, status, failure_reason, created_at, updated_at
                ) VALUES (
                    :id, :idempotencyKey, :fromWalletId, :toWalletId,
                    :amount, :status, :failureReason, :createdAt, :updatedAt
                )
                """,
                params);
    }

    public Optional<Transfer> findById(UUID id) {
        var params = new MapSqlParameterSource("id", id);
        var results =
                jdbc.query(
                        """
                        SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
                               amount, status, failure_reason, created_at, updated_at
                        FROM transfers
                        WHERE id = :id
                        """,
                        params,
                        (rs, rowNum) -> mapTransfer(rs));

        return results.stream().findFirst();
    }

    public Optional<Transfer> findByIdempotencyKey(String idempotencyKey) {
        var params = new MapSqlParameterSource("idempotencyKey", idempotencyKey);
        var results =
                jdbc.query(
                        """
                        SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
                               amount, status, failure_reason, created_at, updated_at
                        FROM transfers
                        WHERE idempotency_key = :idempotencyKey
                        """,
                        params,
                        (rs, rowNum) -> mapTransfer(rs));

        return results.stream().findFirst();
    }

    public void updateStatus(UUID id, TransferStatus status, String failureReason) {
        var params =
                new MapSqlParameterSource()
                        .addValue("id", id)
                        .addValue("status", status.name())
                        .addValue("failureReason", failureReason)
                        .addValue("updatedAt", Timestamp.from(Instant.now()));

        jdbc.update(
                """
                UPDATE transfers
                SET status = :status,
                    failure_reason = :failureReason,
                    updated_at = :updatedAt
                WHERE id = :id
                """,
                params);
    }

    private Transfer mapTransfer(java.sql.ResultSet rs) throws java.sql.SQLException {
        return new Transfer(
                rs.getObject("id", UUID.class),
                rs.getString("idempotency_key"),
                rs.getString("from_wallet_id"),
                rs.getString("to_wallet_id"),
                rs.getLong("amount"),
                TransferStatus.valueOf(rs.getString("status")),
                rs.getString("failure_reason"),
                rs.getTimestamp("created_at").toInstant(),
                rs.getTimestamp("updated_at").toInstant());
    }
}
