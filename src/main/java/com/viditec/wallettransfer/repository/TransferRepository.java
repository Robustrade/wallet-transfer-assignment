package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.domain.TransferState;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.Optional;
import java.util.UUID;

@Repository
public class TransferRepository implements TransferOperations {

    private final JdbcTemplate jdbcTemplate;

    public TransferRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    @Override
    public String createPending(String idempotencyKey, String fromWalletId, String toWalletId, long amount) {
        String transferId = "trf_" + UUID.randomUUID();
        jdbcTemplate.update(
                """
                INSERT INTO transfers (
                    transfer_id,
                    idempotency_key,
                    from_wallet_id,
                    to_wallet_id,
                    amount,
                    state
                )
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                transferId,
                idempotencyKey,
                fromWalletId,
                toWalletId,
                amount,
                TransferState.PENDING.name()
        );
        return transferId;
    }

    @Override
    public void markProcessed(String transferId) {
        jdbcTemplate.update(
                """
                UPDATE transfers
                SET state = ?, failure_reason = NULL, updated_at = CURRENT_TIMESTAMP
                WHERE transfer_id = ?
                """,
                TransferState.PROCESSED.name(),
                transferId
        );
    }

    @Override
    public void markFailed(String transferId, String failureReason) {
        jdbcTemplate.update(
                """
                UPDATE transfers
                SET state = ?, failure_reason = ?, updated_at = CURRENT_TIMESTAMP
                WHERE transfer_id = ?
                """,
                TransferState.FAILED.name(),
                failureReason,
                transferId
        );
    }

    public Optional<TransferRecord> findByIdempotencyKey(String idempotencyKey) {
        return jdbcTemplate.query(
                """
                SELECT transfer_id, idempotency_key, from_wallet_id, to_wallet_id, amount, state
                FROM transfers
                WHERE idempotency_key = ?
                """,
                (rs, rowNum) -> new TransferRecord(
                        rs.getString("transfer_id"),
                        rs.getString("idempotency_key"),
                        rs.getString("from_wallet_id"),
                        rs.getString("to_wallet_id"),
                        rs.getLong("amount"),
                        TransferState.valueOf(rs.getString("state"))
                ),
                idempotencyKey
        ).stream().findFirst();
    }
}
