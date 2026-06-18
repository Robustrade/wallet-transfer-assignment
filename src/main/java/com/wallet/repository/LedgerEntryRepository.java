package com.wallet.repository;

import java.sql.Timestamp;
import java.util.List;
import java.util.UUID;

import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;

@Repository
public class LedgerEntryRepository {

    private static final RowMapper<LedgerEntry> ROW_MAPPER = (rs, rowNum) -> new LedgerEntry(
            UUID.fromString(rs.getString("id")),
            UUID.fromString(rs.getString("transfer_id")),
            UUID.fromString(rs.getString("wallet_id")),
            EntryType.valueOf(rs.getString("entry_type")),
            rs.getBigDecimal("amount"),
            rs.getTimestamp("created_at").toInstant());

    private final NamedParameterJdbcTemplate jdbcTemplate;

    public LedgerEntryRepository(NamedParameterJdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    public void insert(LedgerEntry entry) {
        String sql = "INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at) "
                + "VALUES (:id, :transferId, :walletId, :entryType, :amount, :createdAt)";
        MapSqlParameterSource params = new MapSqlParameterSource()
                .addValue("id", entry.getId())
                .addValue("transferId", entry.getTransferId())
                .addValue("walletId", entry.getWalletId())
                .addValue("entryType", entry.getEntryType().name())
                .addValue("amount", entry.getAmount())
                .addValue("createdAt", Timestamp.from(entry.getCreatedAt()));
        jdbcTemplate.update(sql, params);
    }

    public List<LedgerEntry> findByTransferId(UUID transferId) {
        String sql = "SELECT id, transfer_id, wallet_id, entry_type, amount, created_at "
                + "FROM ledger_entries WHERE transfer_id = :transferId ORDER BY entry_type";
        return jdbcTemplate.query(sql, new MapSqlParameterSource("transferId", transferId), ROW_MAPPER);
    }
}
