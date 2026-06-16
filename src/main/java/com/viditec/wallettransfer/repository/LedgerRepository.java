package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.domain.EntryType;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class LedgerRepository implements LedgerOperations {

    private final JdbcTemplate jdbcTemplate;

    public LedgerRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    @Override
    public void createEntry(String transferId, String walletId, EntryType entryType, long amount) {
        jdbcTemplate.update(
                """
                INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
                VALUES (?, ?, ?, ?)
                """,
                walletId,
                transferId,
                entryType.name(),
                amount
        );
    }
}
