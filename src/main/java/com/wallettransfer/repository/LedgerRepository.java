package com.wallettransfer.repository;

import com.wallettransfer.domain.LedgerEntryType;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.util.UUID;

@Repository
public class LedgerRepository {
    private final JdbcTemplate jdbc;

    public LedgerRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void add(UUID transferId, String walletId, LedgerEntryType type, BigDecimal amount) {
        jdbc.update("INSERT INTO ledger_entries(transfer_id, wallet_id, entry_type, amount) VALUES (?, ?, ?, ?)",
                transferId, walletId, type.name(), amount);
    }
}