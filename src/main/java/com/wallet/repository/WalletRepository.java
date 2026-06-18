package com.wallet.repository;

import java.math.BigDecimal;
import java.sql.Timestamp;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;

import org.springframework.jdbc.core.RowMapper;
import org.springframework.jdbc.core.namedparam.MapSqlParameterSource;
import org.springframework.jdbc.core.namedparam.NamedParameterJdbcTemplate;
import org.springframework.stereotype.Repository;

import com.wallet.domain.Wallet;
import com.wallet.exception.WalletNotFoundException;

@Repository
public class WalletRepository {

    private static final RowMapper<Wallet> ROW_MAPPER = (rs, rowNum) -> new Wallet(
            UUID.fromString(rs.getString("id")),
            rs.getBigDecimal("balance"),
            rs.getTimestamp("created_at").toInstant(),
            rs.getTimestamp("updated_at").toInstant());

    private final NamedParameterJdbcTemplate jdbcTemplate;

    public WalletRepository(NamedParameterJdbcTemplate jdbcTemplate) {
        this.jdbcTemplate = jdbcTemplate;
    }

    public Wallet insert(BigDecimal balance) {
        UUID id = UUID.randomUUID();
        Instant now = Instant.now();
        String sql = "INSERT INTO wallets (id, balance, created_at, updated_at) "
                + "VALUES (:id, :balance, :createdAt, :updatedAt)";
        MapSqlParameterSource params = new MapSqlParameterSource()
                .addValue("id", id)
                .addValue("balance", balance)
                .addValue("createdAt", Timestamp.from(now))
                .addValue("updatedAt", Timestamp.from(now));
        jdbcTemplate.update(sql, params);
        return new Wallet(id, balance, now, now);
    }

    public Optional<Wallet> findById(UUID id) {
        String sql = "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = :id";
        return jdbcTemplate.query(sql, new MapSqlParameterSource("id", id), ROW_MAPPER)
                .stream()
                .findFirst();
    }

    /**
     * Locks a single wallet row for the rest of the current transaction. Callers locking two
     * wallets (a transfer's source and destination) must call this twice in a globally
     * consistent order (e.g. sorted by id) so that concurrent transfers in opposite directions
     * cannot deadlock on each other.
     */
    public Wallet lockById(UUID id) {
        String sql = "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = :id FOR UPDATE";
        return jdbcTemplate.query(sql, new MapSqlParameterSource("id", id), ROW_MAPPER)
                .stream()
                .findFirst()
                .orElseThrow(() -> new WalletNotFoundException(id));
    }

    public void updateBalance(UUID id, BigDecimal newBalance, Instant now) {
        String sql = "UPDATE wallets SET balance = :balance, updated_at = :updatedAt WHERE id = :id";
        MapSqlParameterSource params = new MapSqlParameterSource()
                .addValue("balance", newBalance)
                .addValue("updatedAt", Timestamp.from(now))
                .addValue("id", id);
        jdbcTemplate.update(sql, params);
    }
}
