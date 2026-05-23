package com.kulu.wallet.repository;

import com.kulu.wallet.domain.Transfer;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;

@Repository
public class TransferRepository {

    @Autowired
    private JdbcTemplate jdbc;

    public Optional<Transfer> findById(String id) {
        List<Transfer> rows = jdbc.query(
            "SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount," +
            "       status, failure_reason, created_at, updated_at" +
            "  FROM transfers WHERE id = ?",
            (rs, n) -> new Transfer(
                rs.getString("id"),
                rs.getString("idempotency_key"),
                rs.getLong("from_wallet_id"),
                rs.getLong("to_wallet_id"),
                rs.getLong("amount"),
                Transfer.TransferStatus.valueOf(rs.getString("status")),
                rs.getString("failure_reason"),
                rs.getTimestamp("created_at").toLocalDateTime(),
                rs.getTimestamp("updated_at").toLocalDateTime()),
            id);
        return rows.isEmpty() ? Optional.empty() : Optional.of(rows.get(0));
    }

    public void insert(String id, String idempotencyKey, long fromWalletId, long toWalletId, long amount) {
        jdbc.update(
            "INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) " +
            "VALUES (?, ?, ?, ?, ?, 'PENDING')",
            id, idempotencyKey, fromWalletId, toWalletId, amount);
    }

    public void markProcessed(String transferId) {
        jdbc.update(
            "UPDATE transfers SET status = 'PROCESSED', updated_at = CURRENT_TIMESTAMP WHERE id = ?",
            transferId);
    }

    public void markFailed(String transferId, String reason) {
        jdbc.update(
            "UPDATE transfers SET status = 'FAILED', failure_reason = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
            reason, transferId);
    }
}
