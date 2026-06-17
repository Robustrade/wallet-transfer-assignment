package com.example.wallettransfer.persistence;

import com.example.wallettransfer.application.CreateTransferCommand;
import com.example.wallettransfer.domain.LedgerEntryType;
import com.example.wallettransfer.domain.Transfer;
import com.example.wallettransfer.domain.TransferState;
import com.example.wallettransfer.domain.Wallet;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.sql.ResultSet;
import java.sql.SQLException;
import java.time.OffsetDateTime;
import java.util.List;
import java.util.Optional;
import java.util.UUID;

@Repository
public class TransferRepository {

    private final JdbcTemplate jdbcTemplate;

    public TransferRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    public Optional<Transfer> insertPendingTransferIfAbsent(
            UUID transferId,
            CreateTransferCommand command,
            String requestHash) {
        try {
            OffsetDateTime now = OffsetDateTime.now();
            jdbcTemplate.update(
                    """
                            insert into transfers (
                                id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
                                amount, state, created_at, updated_at
                            )
                            values (?, ?, ?, ?, ?, ?, ?, ?, ?)
                            """,
                    transferId,
                    command.idempotencyKey(),
                    requestHash,
                    command.fromWalletId(),
                    command.toWalletId(),
                    command.amount(),
                    TransferState.PENDING.name(),
                    now,
                    now);
            return findById(transferId);
        } catch (DuplicateKeyException ex) {
            return Optional.empty();
        }
    }

    public Optional<Transfer> findByIdempotencyKey(String idempotencyKey) {
        return jdbcTemplate.query(
                        """
                                select id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
                                       amount, state, failure_reason
                                from transfers
                                where idempotency_key = ?
                                """,
                        this::mapTransfer,
                        idempotencyKey)
                .stream()
                .findFirst();
    }

    public List<Wallet> lockWallets(String firstWalletId, String secondWalletId) {
        return jdbcTemplate.query(
                """
                        select id, balance
                        from wallets
                        where id in (?, ?)
                        order by id
                        for update
                        """,
                this::mapWallet,
                firstWalletId,
                secondWalletId);
    }

    public void updateWalletBalance(String walletId, long balance) {
        jdbcTemplate.update(
                """
                        update wallets
                        set balance = ?, updated_at = ?
                        where id = ?
                        """,
                balance,
                OffsetDateTime.now(),
                walletId);
    }

    public void insertLedgerEntry(UUID transferId, String walletId, LedgerEntryType entryType, long amount) {
        jdbcTemplate.update(
                """
                        insert into ledger_entries (transfer_id, wallet_id, entry_type, amount, created_at)
                        values (?, ?, ?, ?, ?)
                        """,
                transferId,
                walletId,
                entryType.name(),
                amount,
                OffsetDateTime.now());
    }

    public Transfer markProcessed(UUID transferId) {
        return updateTransferState(transferId, TransferState.PROCESSED, null);
    }

    public Transfer markFailed(UUID transferId, String reason) {
        return updateTransferState(transferId, TransferState.FAILED, reason);
    }

    public Optional<Transfer> findById(UUID transferId) {
        return jdbcTemplate.query(
                        """
                                select id, idempotency_key, request_hash, from_wallet_id, to_wallet_id,
                                       amount, state, failure_reason
                                from transfers
                                where id = ?
                                """,
                        this::mapTransfer,
                        transferId)
                .stream()
                .findFirst();
    }

    public Optional<Wallet> findWalletById(String walletId) {
        return jdbcTemplate.query(
                        """
                                select id, balance
                                from wallets
                                where id = ?
                                """,
                        this::mapWallet,
                        walletId)
                .stream()
                .findFirst();
    }

    private Transfer updateTransferState(UUID transferId, TransferState state, String failureReason) {
        jdbcTemplate.update(
                """
                        update transfers
                        set state = ?, failure_reason = ?, updated_at = ?
                        where id = ? and state = ?
                        """,
                state.name(),
                failureReason,
                OffsetDateTime.now(),
                transferId,
                TransferState.PENDING.name());
        return findById(transferId)
                .orElseThrow(() -> new IllegalStateException("Transfer not found after state update"));
    }

    private Transfer mapTransfer(ResultSet rs, int rowNum) throws SQLException {
        return new Transfer(
                rs.getObject("id", UUID.class),
                rs.getString("idempotency_key"),
                rs.getString("request_hash"),
                rs.getString("from_wallet_id"),
                rs.getString("to_wallet_id"),
                rs.getLong("amount"),
                TransferState.valueOf(rs.getString("state")),
                rs.getString("failure_reason"));
    }

    private Wallet mapWallet(ResultSet rs, int rowNum) throws SQLException {
        return new Wallet(rs.getString("id"), rs.getLong("balance"));
    }
}
