package com.kulu.wallet.repository;

import com.kulu.wallet.domain.Wallet;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.List;
import java.util.Optional;

@Repository
public class WalletRepository {

    @Autowired
    private JdbcTemplate jdbc;

    public Optional<Wallet> findById(long id) {
        List<Wallet> rows = jdbc.query(
            "SELECT id, balance, created_at FROM wallets WHERE id = ?",
            (rs, n) -> new Wallet(
                rs.getLong("id"),
                rs.getLong("balance"),
                rs.getTimestamp("created_at").toLocalDateTime()),
            id);
        return rows.isEmpty() ? Optional.empty() : Optional.of(rows.get(0));
    }

    public List<Wallet> lockForUpdate(long id1, long id2) {
        return jdbc.query(
            "SELECT id, balance, created_at FROM wallets WHERE id IN (?, ?) ORDER BY id FOR UPDATE",
            (rs, n) -> new Wallet(
                rs.getLong("id"),
                rs.getLong("balance"),
                rs.getTimestamp("created_at").toLocalDateTime()),
            id1, id2);
    }

    public void debit(long walletId, long amount) {
        jdbc.update("UPDATE wallets SET balance = balance - ? WHERE id = ?", amount, walletId);
    }

    public void credit(long walletId, long amount) {
        jdbc.update("UPDATE wallets SET balance = balance + ? WHERE id = ?", amount, walletId);
    }
}
