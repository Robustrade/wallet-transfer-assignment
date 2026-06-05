package com.example.wallet.repository.impl;

import com.example.wallet.model.Wallet;
import com.example.wallet.repository.WalletRepository;
import lombok.RequiredArgsConstructor;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

import java.math.BigDecimal;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

@Repository
@RequiredArgsConstructor
public class WalletRepositoryImpl implements WalletRepository {

    private final JdbcClient jdbcClient;

    @Override
    public Optional<Wallet> findById(String walletId) {
        return jdbcClient.sql("""
                        SELECT id, balance, created_at
                        FROM wallets
                        WHERE id = :id
                        """)
                .param("id", walletId)
                .query(WalletRepositoryImpl::mapRow)
                .optional();
    }

    @Override
    public List<Wallet> lockForUpdate(List<String> walletIds) {
        if (walletIds.isEmpty()) {
            return Collections.emptyList();
        }

        // JdbcClient passes the List to NamedParameterJdbcTemplate which expands it
        // into positional bind parameters — no string concatenation, no SQL injection risk.
        // ORDER BY id enforces consistent lock ordering at the DB level, preventing deadlocks
        // even if the caller passes IDs in any order.
        return jdbcClient.sql("""
                        SELECT id, balance, created_at
                        FROM wallets
                        WHERE id IN (:ids)
                        ORDER BY id
                        FOR UPDATE
                        """)
                .param("ids", walletIds)
                .query(WalletRepositoryImpl::mapRow)
                .list();
    }

    @Override
    public void debit(String walletId, BigDecimal amount) {
        jdbcClient.sql("""
                        UPDATE wallets
                        SET balance = balance - :amount
                        WHERE id = :id
                        """)
                .param("amount", amount)
                .param("id", walletId)
                .update();
    }

    @Override
    public void credit(String walletId, BigDecimal amount) {
        jdbcClient.sql("""
                        UPDATE wallets
                        SET balance = balance + :amount
                        WHERE id = :id
                        """)
                .param("amount", amount)
                .param("id", walletId)
                .update();
    }

    private static Wallet mapRow(ResultSet rs, int rowNum) throws SQLException {
        return new Wallet(
                rs.getString("id"),
                rs.getBigDecimal("balance"),
                rs.getTimestamp("created_at").toInstant()
        );
    }
}
