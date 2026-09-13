package com.wallettransfer.repository;

import com.wallettransfer.api.TransferResponse;
import com.wallettransfer.domain.TransferStatus;
import org.springframework.dao.EmptyResultDataAccessException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Propagation;
import org.springframework.transaction.annotation.Transactional;

import java.math.BigDecimal;
import java.time.OffsetDateTime;
import java.util.UUID;

@Repository
public class TransferRepository {
    private final JdbcTemplate jdbc;

    public TransferRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public boolean claimIdempotency(String key, UUID transferId, String from, String to, BigDecimal amount) {
        return jdbc.update("""
                INSERT INTO transfers(id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
                VALUES (?, ?, ?, ?, ?, 'PENDING') ON CONFLICT (idempotency_key) DO NOTHING
                """, transferId, key, from, to, amount) == 1;
    }

    public TransferResponse findByIdempotencyKey(String key) {
        try {
            return jdbc.queryForObject("""
                    SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at
                    FROM transfers WHERE idempotency_key = ?
                    """, (rs, rowNum) -> new TransferResponse(rs.getObject("id", UUID.class),
                    rs.getString("idempotency_key"), rs.getString("from_wallet_id"), rs.getString("to_wallet_id"),
                    rs.getBigDecimal("amount"), TransferStatus.valueOf(rs.getString("status")),
                    rs.getObject("created_at", OffsetDateTime.class)), key);
        } catch (EmptyResultDataAccessException exception) {
            return null;
        }
    }

    public TransferResponse findByIdempotencyKeyWithLock(String key) {
        jdbc.queryForObject("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", Long.class, key);
        return findByIdempotencyKey(key);
    }

    public void markProcessed(UUID transferId) {
        jdbc.update("UPDATE transfers SET status = 'PROCESSED', updated_at = CURRENT_TIMESTAMP WHERE id = ?", transferId);
    }

    @Transactional(propagation = Propagation.REQUIRES_NEW)
    public void markFailed(UUID transferId, String reason) {
        jdbc.update("UPDATE transfers SET status = 'FAILED', failure_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
                reason, transferId);
    }

    public TransferResponse findById(UUID transferId) {
        return jdbc.queryForObject("""
                SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at
                FROM transfers WHERE id = ?
                """, (rs, rowNum) -> new TransferResponse(rs.getObject("id", UUID.class),
                rs.getString("idempotency_key"), rs.getString("from_wallet_id"), rs.getString("to_wallet_id"),
                rs.getBigDecimal("amount"), TransferStatus.valueOf(rs.getString("status")),
                rs.getObject("created_at", OffsetDateTime.class)), transferId);
    }
}