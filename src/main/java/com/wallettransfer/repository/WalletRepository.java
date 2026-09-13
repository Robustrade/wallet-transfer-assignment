package com.wallettransfer.repository;

import com.wallettransfer.api.WalletResponse;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.util.UUID;

@Repository
public class WalletRepository {
    private final JdbcTemplate jdbc;

    public WalletRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public WalletResponse findByIdForUpdate(String walletId) {
        return jdbc.queryForObject("SELECT id, balance FROM wallets WHERE id = ? FOR UPDATE", (rs, rowNum) ->
                new WalletResponse(rs.getString("id"), rs.getBigDecimal("balance")), walletId);
    }

    public WalletResponse findById(String walletId) {
        return jdbc.queryForObject("SELECT id, balance FROM wallets WHERE id = ?", (rs, rowNum) ->
                new WalletResponse(rs.getString("id"), rs.getBigDecimal("balance")), walletId);
    }

    public WalletResponse create(BigDecimal initialBalance) {
        String walletId = UUID.randomUUID().toString();
        jdbc.update("INSERT INTO wallets(id, balance) VALUES (?, ?)", walletId, initialBalance);
        return new WalletResponse(walletId, initialBalance);
    }

    public void updateBalance(String walletId, BigDecimal balance) {
        jdbc.update("UPDATE wallets SET balance = ?, version = version + 1 WHERE id = ?", balance, walletId);
    }
}