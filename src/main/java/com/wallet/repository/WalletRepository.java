package com.wallet.repository;

import com.wallet.domain.Wallet;
import java.sql.Timestamp;
import java.util.Optional;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

@Repository
public class WalletRepository {

    private final NamedParameterJdbcTemplate jdbc;

    public WalletRepository(NamedParameterJdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public Optional<Wallet> findById(String id) {
        var params = new MapSqlParameterSource("id", id);
        var results =
                jdbc.query(
                        """
                        SELECT id, balance, version, created_at
                        FROM wallets
                        WHERE id = :id
                        """,
                        params,
                        (rs, rowNum) ->
                                new Wallet(
                                        rs.getString("id"),
                                        rs.getLong("balance"),
                                        rs.getLong("version"),
                                        rs.getTimestamp("created_at").toInstant()));

        return results.stream().findFirst();
    }

    public Optional<Wallet> findByIdForUpdate(String id) {
        var params = new MapSqlParameterSource("id", id);
        var results =
                jdbc.query(
                        """
                        SELECT id, balance, version, created_at
                        FROM wallets
                        WHERE id = :id
                        FOR UPDATE
                        """,
                        params,
                        (rs, rowNum) ->
                                new Wallet(
                                        rs.getString("id"),
                                        rs.getLong("balance"),
                                        rs.getLong("version"),
                                        rs.getTimestamp("created_at").toInstant()));

        return results.stream().findFirst();
    }

    public void debit(String walletId, long amount) {
        var params =
                new MapSqlParameterSource()
                        .addValue("walletId", walletId)
                        .addValue("amount", amount);
        int updated =
                jdbc.update(
                        """
                        UPDATE wallets
                        SET balance = balance - :amount,
                            version = version + 1
                        WHERE id = :walletId
                        """,
                        params);
        if (updated != 1) {
            throw new IllegalStateException("Failed to debit wallet: " + walletId);
        }
    }

    public void credit(String walletId, long amount) {
        var params =
                new MapSqlParameterSource()
                        .addValue("walletId", walletId)
                        .addValue("amount", amount);
        int updated =
                jdbc.update(
                        """
                        UPDATE wallets
                        SET balance = balance + :amount,
                            version = version + 1
                        WHERE id = :walletId
                        """,
                        params);
        if (updated != 1) {
            throw new IllegalStateException("Failed to credit wallet: " + walletId);
        }
    }

    public void insert(String id, long balance) {
        var params =
                new MapSqlParameterSource()
                        .addValue("id", id)
                        .addValue("balance", balance)
                        .addValue("createdAt", Timestamp.from(java.time.Instant.now()));
        jdbc.update(
                """
                INSERT INTO wallets (id, balance, created_at)
                VALUES (:id, :balance, :createdAt)
                """,
                params);
    }
}
