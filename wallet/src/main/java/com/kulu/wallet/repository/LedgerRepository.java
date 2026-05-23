package com.kulu.wallet.repository;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class LedgerRepository {

    @Autowired
    private JdbcTemplate jdbc;

    public void insertDebit(String transferId, long walletId, long amount) {
        insert(transferId, walletId, "DEBIT", amount);
    }

    public void insertCredit(String transferId, long walletId, long amount) {
        insert(transferId, walletId, "CREDIT", amount);
    }

    private void insert(String transferId, long walletId, String type, long amount) {
        jdbc.update(
            "INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) " +
            "VALUES (?, ?, CAST(? AS entry_type), ?)",
            transferId, walletId, type, amount);
    }
}
