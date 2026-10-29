package com.example.wallet.repository;

import com.example.wallet.domain.Wallet;
import com.example.wallet.domain.WalletAlreadyExistsException;
import java.time.OffsetDateTime;
import java.util.List;
import java.util.Optional;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.jdbc.core.RowMapper;
import org.springframework.stereotype.Repository;

/** Persistence only. Locking and business decisions belong to the service layer. */
@Repository
public class WalletRepository {

    private static final RowMapper<Wallet> MAPPER = (rs, i) -> new Wallet(
            rs.getString("id"),
            rs.getLong("balance"),
            rs.getObject("created_at", OffsetDateTime.class).toInstant(),
            rs.getObject("updated_at", OffsetDateTime.class).toInstant());

    private final JdbcTemplate jdbc;

    public WalletRepository(JdbcTemplate jdbc) {
        this.jdbc = jdbc;
    }

    public void insert(String id, long openingBalance) {
        try {
            jdbc.update("INSERT INTO wallets (id, balance) VALUES (?, ?)", id, openingBalance);
        } catch (DuplicateKeyException e) {
            throw new WalletAlreadyExistsException(id);
        }
    }

    public Optional<Wallet> findById(String id) {
        return jdbc.query("SELECT * FROM wallets WHERE id = ?", MAPPER, id).stream().findFirst();
    }

    /**
     * Row-locks both wallets (SELECT ... FOR UPDATE) in a deterministic order (by id). Always locking
     * in the same order means two opposite transfers (A to B and B to A) cannot deadlock.
     * Must be called inside a transaction; the locks are released on commit or rollback.
     */
    public List<Wallet> lockBothOrderedById(String walletIdA, String walletIdB) {
        return jdbc.query(
                "SELECT * FROM wallets WHERE id IN (?, ?) ORDER BY id FOR UPDATE", MAPPER, walletIdA, walletIdB);
    }

    /** Guarded decrement: refuses to go below zero even if the caller forgot to check. */
    public boolean debit(String walletId, long amount) {
        return jdbc.update(
                        "UPDATE wallets SET balance = balance - ?, updated_at = now() "
                                + "WHERE id = ? AND balance >= ?",
                        amount, walletId, amount)
                == 1;
    }

    public void credit(String walletId, long amount) {
        jdbc.update("UPDATE wallets SET balance = balance + ?, updated_at = now() WHERE id = ?", amount, walletId);
    }
}
