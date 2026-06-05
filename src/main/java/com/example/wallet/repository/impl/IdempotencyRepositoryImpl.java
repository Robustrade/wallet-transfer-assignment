package com.example.wallet.repository.impl;

import com.example.wallet.model.IdempotencyRecord;
import com.example.wallet.repository.IdempotencyRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;

@Repository
@RequiredArgsConstructor
public class IdempotencyRepositoryImpl implements IdempotencyRepository {

    private final JdbcClient jdbcClient;

    @Override
    public Optional<IdempotencyRecord> find(String idempotencyKey, String endpoint) {
        return jdbcClient.sql("""
                        SELECT idempotency_key, endpoint, response_body, http_status,
                               created_at, expires_at
                        FROM idempotency_records
                        WHERE idempotency_key = :key
                          AND endpoint        = :endpoint
                          AND expires_at      > now()
                        """)
                .param("key", idempotencyKey)
                .param("endpoint", endpoint)
                .query(IdempotencyRepositoryImpl::mapRow)
                .optional();
    }

    @Override
    public void save(String idempotencyKey, String endpoint, String responseBody,
                     int httpStatus, Instant expiresAt) {
        // ON CONFLICT DO NOTHING: if two concurrent requests race to save the same key,
        // the second insert is silently dropped — the first committed response wins.
        jdbcClient.sql("""
                        INSERT INTO idempotency_records
                            (idempotency_key, endpoint, response_body, http_status, expires_at)
                        VALUES (:key, :endpoint, :responseBody, :httpStatus, :expiresAt)
                        ON CONFLICT (idempotency_key, endpoint) DO NOTHING
                        """)
                .param("key", idempotencyKey)
                .param("endpoint", endpoint)
                .param("responseBody", responseBody)
                .param("httpStatus", httpStatus)
                .param("expiresAt", Timestamp.from(expiresAt))
                .update();
    }

    @Override
    public int deleteExpired() {
        return jdbcClient.sql("""
                        DELETE FROM idempotency_records
                        WHERE expires_at <= now()
                        """)
                .update();
    }

    private static IdempotencyRecord mapRow(ResultSet rs, int rowNum) throws SQLException {
        return new IdempotencyRecord(
                rs.getString("idempotency_key"),
                rs.getString("endpoint"),
                rs.getString("response_body"),
                rs.getInt("http_status"),
                rs.getTimestamp("created_at").toInstant(),
                rs.getTimestamp("expires_at").toInstant()
        );
    }
}

