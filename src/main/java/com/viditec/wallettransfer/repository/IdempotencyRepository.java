package com.viditec.wallettransfer.repository;

import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.Optional;

@Repository
public class IdempotencyRepository implements IdempotencyOperations {

    private final JdbcTemplate jdbcTemplate;

    public IdempotencyRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    @Override
    public boolean reserve(String idempotencyKey, String requestHash) {
        try {
            jdbcTemplate.update(
                    """
                    INSERT INTO idempotency_records (idempotency_key, request_hash)
                    VALUES (?, ?)
                    """,
                    idempotencyKey,
                    requestHash
            );
            return true;
        } catch (DuplicateKeyException exception) {
            return false;
        }
    }

    @Override
    public Optional<IdempotencyRecord> findByKey(String idempotencyKey) {
        return jdbcTemplate.query(
                """
                SELECT idempotency_key, request_hash, transfer_id, http_status, response_body
                FROM idempotency_records
                WHERE idempotency_key = ?
                """,
                (rs, rowNum) -> new IdempotencyRecord(
                        rs.getString("idempotency_key"),
                        rs.getString("request_hash"),
                        rs.getString("transfer_id"),
                        rs.getObject("http_status", Integer.class),
                        rs.getString("response_body")
                ),
                idempotencyKey
        ).stream().findFirst();
    }

    @Override
    public void complete(String idempotencyKey, String transferId, int httpStatus, String responseBody) {
        jdbcTemplate.update(
                """
                UPDATE idempotency_records
                SET transfer_id = ?, http_status = ?, response_body = ?
                WHERE idempotency_key = ?
                """,
                transferId,
                httpStatus,
                responseBody,
                idempotencyKey
        );
    }
}
