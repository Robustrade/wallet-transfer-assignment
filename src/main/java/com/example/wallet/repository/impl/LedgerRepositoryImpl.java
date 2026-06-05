package com.example.wallet.repository.impl;

import com.example.wallet.model.EntryType;
import com.example.wallet.model.LedgerEntry;
import com.example.wallet.repository.LedgerRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.List;
import java.util.UUID;

@Repository
@RequiredArgsConstructor
public class LedgerRepositoryImpl implements LedgerRepository {

    private final JdbcClient jdbcClient;

    @Override
    public void insertDebit(UUID transferId, String walletId, BigDecimal amount) {
        insertEntry(transferId, walletId, EntryType.DEBIT, amount);
    }

    @Override
    public void insertCredit(UUID transferId, String walletId, BigDecimal amount) {
        insertEntry(transferId, walletId, EntryType.CREDIT, amount);
    }

    @Override
    public List<LedgerEntry> findByTransferId(UUID transferId) {
        return jdbcClient.sql("""
                        SELECT id, transfer_id, wallet_id, type, amount, created_at
                        FROM ledger_entries
                        WHERE transfer_id = :transferId
                        ORDER BY type   -- CREDIT before DEBIT — deterministic ordering
                        """)
                .param("transferId", transferId)
                .query(LedgerRepositoryImpl::mapRow)
                .list();
    }

    @Override
    public int countByTransferId(UUID transferId) {
        return jdbcClient.sql("""
                        SELECT COUNT(*)
                        FROM ledger_entries
                        WHERE transfer_id = :transferId
                        """)
                .param("transferId", transferId)
                .query(Integer.class)
                .single();
    }

    // -------------------------------------------------------------------------
    // Private helpers
    // -------------------------------------------------------------------------

    private void insertEntry(UUID transferId, String walletId, EntryType type, BigDecimal amount) {
        jdbcClient.sql("""
                        INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
                        VALUES (:transferId, :walletId, :type, :amount)
                        """)
                .param("transferId", transferId)
                .param("walletId", walletId)
                .param("type", type.name())
                .param("amount", amount)
                .update();
    }

    private static LedgerEntry mapRow(ResultSet rs, int rowNum) throws SQLException {
        return new LedgerEntry(
                UUID.fromString(rs.getString("id")),
                UUID.fromString(rs.getString("transfer_id")),
                rs.getString("wallet_id"),
                EntryType.valueOf(rs.getString("type")),
                rs.getBigDecimal("amount"),
                rs.getTimestamp("created_at").toInstant()
        );
    }
}
