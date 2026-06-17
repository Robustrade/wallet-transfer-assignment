package com.wallet.repository;

import com.wallet.domain.IdempotencyRecord;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class IdempotencyRepository {

    private final NamedParameterJdbcTemplate jdbc;

    public IdempotencyRepository(NamedParameterJdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public boolean tryInsert(String idempotencyKey, UUID transferId, String requestHash) {
        var params =
                new MapSqlParameterSource()
                        .addValue("idempotencyKey", idempotencyKey)
                        .addValue("transferId", transferId)
                        .addValue("requestHash", requestHash)
                        .addValue("responseBody", "{}")
                        .addValue("httpStatus", 0)
                        .addValue("createdAt", Timestamp.from(Instant.now()));

        try {
            int inserted =
                    jdbc.update(
                            """
                            INSERT INTO idempotency_records (
                                idempotency_key, transfer_id, request_hash,
                                response_body, http_status, created_at
                            ) VALUES (
                                :idempotencyKey, :transferId, :requestHash,
                                CAST(:responseBody AS JSONB), :httpStatus, :createdAt
                            )
                            """,
                            params);
            return inserted == 1;
        } catch (DuplicateKeyException ex) {
            return false;
        }
    }

    public Optional<IdempotencyRecord> findByKey(String idempotencyKey) {
        var params = new MapSqlParameterSource("idempotencyKey", idempotencyKey);
        var results =
                jdbc.query(
                        """
                        SELECT idempotency_key, transfer_id, request_hash,
                               response_body::text AS response_body, http_status, created_at
                        FROM idempotency_records
                        WHERE idempotency_key = :idempotencyKey
                        """,
                        params,
                        (rs, rowNum) ->
                                new IdempotencyRecord(
                                        rs.getString("idempotency_key"),
                                        rs.getObject("transfer_id", UUID.class),
                                        rs.getString("request_hash"),
                                        rs.getString("response_body"),
                                        rs.getInt("http_status"),
                                        rs.getTimestamp("created_at").toInstant()));

        return results.stream().findFirst();
    }

    public void updateResponse(
            String idempotencyKey, String responseBody, int httpStatus) {
        var params =
                new MapSqlParameterSource()
                        .addValue("idempotencyKey", idempotencyKey)
                        .addValue("responseBody", responseBody)
                        .addValue("httpStatus", httpStatus);

        jdbc.update(
                """
                UPDATE idempotency_records
                SET response_body = CAST(:responseBody AS JSONB),
                    http_status = :httpStatus
                WHERE idempotency_key = :idempotencyKey
                """,
                params);
    }
}
