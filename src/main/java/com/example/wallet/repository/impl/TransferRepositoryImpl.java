package com.example.wallet.repository.impl;

import com.example.wallet.exception.InvalidTransferStateException;
import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.model.Transfer;
import com.example.wallet.model.TransferStatus;
import com.example.wallet.repository.TransferRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

@Repository
@RequiredArgsConstructor
public class TransferRepositoryImpl implements TransferRepository {

    private final JdbcClient jdbcClient;

    @Override
    public Optional<Transfer> findByIdempotencyKey(String idempotencyKey) {
        return jdbcClient.sql("""
                        SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
                               amount, status, created_at, updated_at
                        FROM transfers
                        WHERE idempotency_key = :key
                        """)
                .param("key", idempotencyKey)
                .query(TransferRepositoryImpl::mapRow)
                .optional();
    }

    @Override
    public Optional<Transfer> findById(UUID id) {
        return jdbcClient.sql("""
                        SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
                               amount, status, created_at, updated_at
                        FROM transfers
                        WHERE id = :id
                        """)
                .param("id", id)
                .query(TransferRepositoryImpl::mapRow)
                .optional();
    }

    @Override
    public Transfer insertPending(CreateTransferRequest request) {
        // Try to insert — do nothing on conflict (concurrent duplicate request)
        jdbcClient.sql("""
                        INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status)
                        VALUES (:key, :from, :to, :amount, 'PENDING')
                        ON CONFLICT (idempotency_key) DO NOTHING
                        """)
                .param("key", request.idempotencyKey())
                .param("from", request.fromWalletId())
                .param("to", request.toWalletId())
                .param("amount", request.amount())
                .update();

        // Always re-read the winning row — whether we just inserted it or it already existed
        return findByIdempotencyKey(request.idempotencyKey())
                .orElseThrow(() -> new IllegalStateException(
                        "Transfer not found after insert for idempotencyKey=" + request.idempotencyKey()));
    }

    @Override
    public List<Transfer> findStuckPending(Instant stuckBefore) {
        return jdbcClient.sql("""
                        SELECT id, idempotency_key, from_wallet_id, to_wallet_id,
                               amount, status, created_at, updated_at
                        FROM transfers
                        WHERE status     = 'PENDING'
                          AND created_at < :stuckBefore
                        ORDER BY created_at
                        """)
                .param("stuckBefore", Timestamp.from(stuckBefore))
                .query(TransferRepositoryImpl::mapRow)
                .list();
    }

    @Override
    public void updateStatus(UUID transferId, TransferStatus status) {
        // Guard: only transition from PENDING — prevents concurrent retries from
        // overwriting a terminal state (PROCESSED or FAILED) with a different value.
        int updated = jdbcClient.sql("""
                        UPDATE transfers
                        SET status = :status, updated_at = now()
                        WHERE id = :id
                          AND status = 'PENDING'
                        """)
                .param("status", status.name())
                .param("id", transferId)
                .update();

        if (updated == 0) {
            throw new InvalidTransferStateException(transferId, status);
        }
    }

    private static Transfer mapRow(ResultSet rs, int rowNum) throws SQLException {
        return new Transfer(
                UUID.fromString(rs.getString("id")),
                rs.getString("idempotency_key"),
                rs.getString("from_wallet_id"),
                rs.getString("to_wallet_id"),
                rs.getBigDecimal("amount"),
                TransferStatus.valueOf(rs.getString("status")),
                rs.getTimestamp("created_at").toInstant(),
                rs.getTimestamp("updated_at").toInstant()
        );
    }
}
