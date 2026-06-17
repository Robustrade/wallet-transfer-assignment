package com.wallet.repository;

import com.wallet.domain.LedgerEntry;
import com.wallet.domain.LedgerEntryType;
import java.util.List;
import java.util.UUID;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class LedgerRepository {

    private final NamedParameterJdbcTemplate jdbc;

    public LedgerRepository(NamedParameterJdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(String walletId, UUID transferId, LedgerEntryType entryType, long amount) {
        var params =
                new MapSqlParameterSource()
                        .addValue("walletId", walletId)
                        .addValue("transferId", transferId)
                        .addValue("entryType", entryType.name())
                        .addValue("amount", amount);

        jdbc.update(
                """
                INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
                VALUES (:walletId, :transferId, :entryType, :amount)
                """,
                params);
    }

    public List<LedgerEntry> findByTransferId(UUID transferId) {
        var params = new MapSqlParameterSource("transferId", transferId);
        return jdbc.query(
                """
                SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
                FROM ledger_entries
                WHERE transfer_id = :transferId
                ORDER BY entry_type
                """,
                params,
                (rs, rowNum) ->
                        new LedgerEntry(
                                rs.getLong("id"),
                                rs.getString("wallet_id"),
                                rs.getObject("transfer_id", UUID.class),
                                LedgerEntryType.valueOf(rs.getString("entry_type")),
                                rs.getLong("amount"),
                                rs.getTimestamp("created_at").toInstant()));
    }

    public long countByTransferId(UUID transferId) {
        var params = new MapSqlParameterSource("transferId", transferId);
        Long count =
                jdbc.queryForObject(
                        """
                        SELECT COUNT(*)
                        FROM ledger_entries
                        WHERE transfer_id = :transferId
                        """,
                        params,
                        Long.class);
        return count == null ? 0L : count;
    }
}
