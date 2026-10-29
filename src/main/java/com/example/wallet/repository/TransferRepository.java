package com.example.wallet.repository;

import com.example.wallet.domain.Transfer;
import com.example.wallet.domain.TransferStatus;
import java.time.OffsetDateTime;
import java.util.List;
import java.util.Optional;
import java.util.UUID;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

@Repository
public class TransferRepository {

    private static final RowMapper<Transfer> MAPPER = (rs, i) -> new Transfer(
            rs.getObject("id", UUID.class),
            rs.getString("idempotency_key"),
            rs.getString("request_hash"),
            rs.getString("from_wallet_id"),
            rs.getString("to_wallet_id"),
            rs.getLong("amount"),
            TransferStatus.valueOf(rs.getString("status")),
            rs.getString("failure_reason"),
            rs.getObject("created_at", OffsetDateTime.class).toInstant(),
            rs.getObject("updated_at", OffsetDateTime.class).toInstant());

    private final JdbcTemplate jdbc;

    public TransferRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    /**
     * Atomically claims the idempotency key by inserting a PENDING transfer.
     *
     * <p>ON CONFLICT DO NOTHING means a duplicate key never raises an error (which would abort the
     * Postgres transaction). If another transaction is inserting the same key right now, this statement
     * blocks until that transaction commits or rolls back.
     *
     * @return true if this call created the transfer, false if the key already existed
     */
    public boolean insertPendingIfKeyAbsent(Transfer t) {
        return jdbc.update(
                        "INSERT INTO transfers (id, idempotency_key, request_hash, from_wallet_id, to_wallet_id, "
                                + "amount, status) VALUES (?, ?, ?, ?, ?, ?, ?) "
                                + "ON CONFLICT (idempotency_key) DO NOTHING",
                        t.id(),
                        t.idempotencyKey(),
                        t.requestHash(),
                        t.fromWalletId(),
                        t.toWalletId(),
                        t.amount(),
                        t.status().name())
                == 1;
    }

    public Optional<Transfer> findById(UUID id) {
        return jdbc.query("SELECT * FROM transfers WHERE id = ?", MAPPER, id).stream().findFirst();
    }

    public Optional<Transfer> findByIdempotencyKey(String key) {
        return jdbc.query("SELECT * FROM transfers WHERE idempotency_key = ?", MAPPER, key).stream()
                .findFirst();
    }

    /**
     * Compare-and-set state change: only succeeds if the row is still in {@code expected}. This makes
     * the transition safe against retries and duplicate workers.
     */
    public boolean transition(UUID id, TransferStatus expected, TransferStatus next, String failureReason) {
        return jdbc.update(
                        "UPDATE transfers SET status = ?, failure_reason = ?, updated_at = now() "
                                + "WHERE id = ? AND status = ?",
                        next.name(), failureReason, id, expected.name())
                == 1;
    }

    public List<Transfer> findByWallet(String walletId, int limit) {
        return jdbc.query(
                "SELECT * FROM transfers WHERE from_wallet_id = ? OR to_wallet_id = ? "
                        + "ORDER BY created_at DESC, id LIMIT ?",
                MAPPER, walletId, walletId, limit);
    }
}
