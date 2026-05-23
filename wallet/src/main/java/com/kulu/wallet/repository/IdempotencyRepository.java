package com.kulu.wallet.repository;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.List;

@Repository
public class IdempotencyRepository {

    @Autowired
    private JdbcTemplate jdbc;

    public boolean claim(String key) {
        int rows = jdbc.update(
            "INSERT INTO idempotency_records (idempotency_key, response_status, response_body) " +
            "VALUES (?, 0, 'PROCESSING') ON CONFLICT DO NOTHING",
            key);
        return rows > 0;
    }

    public IdempotencyRecord find(String key) {
        List<IdempotencyRecord> records = jdbc.query(
            "SELECT response_status, response_body FROM idempotency_records WHERE idempotency_key = ?",
            (rs, n) -> new IdempotencyRecord(rs.getInt("response_status"), rs.getString("response_body")),
            key);
        return records.isEmpty() ? null : records.get(0);
    }

    public void resolve(String key, int status, String body) {
        jdbc.update(
            "UPDATE idempotency_records SET response_status = ?, response_body = ? WHERE idempotency_key = ?",
            status, body, key);
    }

    public record IdempotencyRecord(int responseStatus, String responseBody) {}
}
