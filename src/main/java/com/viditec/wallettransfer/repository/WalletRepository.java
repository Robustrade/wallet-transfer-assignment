package com.viditec.wallettransfer.repository;

import com.viditec.wallettransfer.dto.WalletDto;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Repository;

import java.util.Optional;

@Repository
public class WalletRepository implements WalletOperations {

    private final JdbcTemplate jdbcTemplate;

    public WalletRepository(JdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    @Override
    public void createWallet(String walletId, long initialBalance) {
        try {
            jdbcTemplate.update(
                    """
                    INSERT INTO wallets (wallet_id, balance)
                    VALUES (?, ?)
                    """,
                    walletId,
                    initialBalance
            );
        } catch (DuplicateKeyException exception) {
            throw exception;
        }
    }

    @Override
    public Optional<WalletDto> findById(String walletId) {
        return jdbcTemplate.query(
                """
                SELECT wallet_id, balance
                FROM wallets
                WHERE wallet_id = ?
                """,
                (rs, rowNum) -> new WalletDto(rs.getString("wallet_id"), rs.getLong("balance")),
                walletId
        ).stream().findFirst();
    }

    @Override
    public boolean exists(String walletId) {
        Integer count = jdbcTemplate.queryForObject(
                """
                SELECT COUNT(*)
                FROM wallets
                WHERE wallet_id = ?
                """,
                Integer.class,
                walletId
        );
        return count != null && count > 0;
    }

    @Override
    public int debitIfSufficientFunds(String walletId, long amount) {
        return jdbcTemplate.update(
                """
                UPDATE wallets
                SET balance = balance - ?,
                updated_at = CURRENT_TIMESTAMP
                WHERE wallet_id = ?
                  AND balance >= ?
                """,
                amount,
                walletId,
                amount
        );
    }

    @Override
    public int credit(String walletId, long amount) {
        return jdbcTemplate.update(
                """
                UPDATE wallets
                SET balance = balance + ?,
                updated_at = CURRENT_TIMESTAMP
                WHERE wallet_id = ?
                """,
                amount,
                walletId
        );
    }
}
