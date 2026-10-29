package com.example.wallet.repository;

import com.example.wallet.domain.EntryType;
import com.example.wallet.domain.LedgerEntry;
import java.time.OffsetDateTime;
import java.util.List;
import java.util.UUID;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

@Repository
public class LedgerRepository {

    private static final RowMapper<LedgerEntry> MAPPER = (rs, i) -> new LedgerEntry(
            rs.getLong("id"),
            rs.getString("wallet_id"),
            rs.getObject("transfer_id", UUID.class),
            EntryType.valueOf(rs.getString("entry_type")),
            rs.getLong("amount"),
            rs.getObject("created_at", OffsetDateTime.class).toInstant());

    private final JdbcTemplate jdbc;

    public LedgerRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(String walletId, UUID transferId, EntryType type, long amount) {
        jdbc.update(
                "INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount) VALUES (?, ?, ?, ?)",
                walletId, transferId, type.name(), amount);
    }

    public List<LedgerEntry> findByTransferId(UUID transferId) {
        return jdbc.query("SELECT * FROM ledger_entries WHERE transfer_id = ? ORDER BY id", MAPPER, transferId);
    }

    public List<LedgerEntry> findByWallet(String walletId) {
        return jdbc.query("SELECT * FROM ledger_entries WHERE wallet_id = ? ORDER BY id", MAPPER, walletId);
    }
}
