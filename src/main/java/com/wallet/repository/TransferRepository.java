package com.wallet.repository;

import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;

import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;

@Repository
public class TransferRepository {

    private static final RowMapper<Transfer> ROW_MAPPER = (rs, rowNum) -> new Transfer(
            UUID.fromString(rs.getString("id")),
            rs.getString("idempotency_key"),
            rs.getString("request_fingerprint"),
            UUID.fromString(rs.getString("from_wallet_id")),
            UUID.fromString(rs.getString("to_wallet_id")),
            rs.getBigDecimal("amount"),
            TransferStatus.valueOf(rs.getString("status")),
            rs.getString("failure_reason"),
            rs.getTimestamp("created_at").toInstant(),
            rs.getTimestamp("updated_at").toInstant());

    private static final String SELECT_COLUMNS = "id, idempotency_key, request_fingerprint, "
            + "from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at";

    private final NamedParameterJdbcTemplate jdbcTemplate;

    public TransferRepository(NamedParameterJdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    /**
     * Inserts a new transfer in PENDING status. Relies on the unique constraint on
     * idempotency_key to atomically reject concurrent duplicate submissions; callers should
     * catch {@link org.springframework.dao.DuplicateKeyException} and treat it as a replay.
     */
    public void insertPending(Transfer transfer) {
        String sql = "INSERT INTO transfers (id, idempotency_key, request_fingerprint, from_wallet_id, "
                + "to_wallet_id, amount, status, failure_reason, created_at, updated_at) "
                + "VALUES (:id, :idempotencyKey, :requestFingerprint, :fromWalletId, :toWalletId, :amount, "
                + ":status, :failureReason, :createdAt, :updatedAt)";
        jdbcTemplate.update(sql, toParams(transfer));
    }

    public Optional<Transfer> findByIdempotencyKey(String idempotencyKey) {
        String sql = "SELECT " + SELECT_COLUMNS + " FROM transfers WHERE idempotency_key = :idempotencyKey";
        return jdbcTemplate
                .query(sql, new MapSqlParameterSource("idempotencyKey", idempotencyKey), ROW_MAPPER)
                .stream()
                .findFirst();
    }

    public Optional<Transfer> findById(UUID id) {
        String sql = "SELECT " + SELECT_COLUMNS + " FROM transfers WHERE id = :id";
        return jdbcTemplate.query(sql, new MapSqlParameterSource("id", id), ROW_MAPPER).stream().findFirst();
    }

    public void updateStatus(Transfer transfer) {
        String sql = "UPDATE transfers SET status = :status, failure_reason = :failureReason, "
                + "updated_at = :updatedAt WHERE id = :id";
        MapSqlParameterSource params = new MapSqlParameterSource()
                .addValue("status", transfer.getStatus().name())
                .addValue("failureReason", transfer.getFailureReason())
                .addValue("updatedAt", Timestamp.from(transfer.getUpdatedAt()))
                .addValue("id", transfer.getId());
        jdbcTemplate.update(sql, params);
    }

    private MapSqlParameterSource toParams(Transfer transfer) {
        Instant now = transfer.getCreatedAt();
        return new MapSqlParameterSource()
                .addValue("id", transfer.getId())
                .addValue("idempotencyKey", transfer.getIdempotencyKey())
                .addValue("requestFingerprint", transfer.getRequestFingerprint())
                .addValue("fromWalletId", transfer.getFromWalletId())
                .addValue("toWalletId", transfer.getToWalletId())
                .addValue("amount", transfer.getAmount())
                .addValue("status", transfer.getStatus().name())
                .addValue("failureReason", transfer.getFailureReason())
                .addValue("createdAt", Timestamp.from(now))
                .addValue("updatedAt", Timestamp.from(transfer.getUpdatedAt()));
    }
}
