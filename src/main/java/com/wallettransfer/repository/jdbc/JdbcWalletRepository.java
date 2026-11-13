package com.wallettransfer.repository.jdbc;

import com.wallettransfer.domain.Wallet;
import com.wallettransfer.domain.WalletAlreadyExistsException;
import com.wallettransfer.repository.WalletRepository;
import java.time.Instant;
import java.util.List;
import java.util.Optional;
import org.springframework.dao.DuplicateKeyException;
import org.springframework.jdbc.core.simple.JdbcClient;
import org.springframework.stereotype.Repository;

/** PostgreSQL implementation of {@link WalletRepository}. */
@Repository
class JdbcWalletRepository implements WalletRepository {

  private final JdbcClient jdbc;

  JdbcWalletRepository(JdbcClient jdbc) {
    this.jdbc = jdbc;
  }

  @Override
  public void create(Wallet wallet) {
    try {
      jdbc.sql(
              """
              INSERT INTO wallets (id, balance, opening_balance, created_at, updated_at)
              VALUES (:id, :balance, :opening, :createdAt, :updatedAt)
              """)
          .param("id", wallet.id())
          .param("balance", wallet.balance())
          .param("opening", wallet.openingBalance())
          .param("createdAt", Timestamps.toDb(wallet.createdAt()))
          .param("updatedAt", Timestamps.toDb(wallet.updatedAt()))
          .update();
    } catch (DuplicateKeyException e) {
      throw new WalletAlreadyExistsException(wallet.id(), e);
    }
  }

  @Override
  public Optional<Wallet> findById(String id) {
    return jdbc.sql(
            """
            SELECT id, balance, opening_balance, created_at, updated_at
              FROM wallets WHERE id = :id
            """)
        .param("id", id)
        .query(
            (rs, rowNum) ->
                new Wallet(
                    rs.getString("id"),
                    rs.getLong("balance"),
                    rs.getLong("opening_balance"),
                    Timestamps.read(rs, "created_at"),
                    Timestamps.read(rs, "updated_at")))
        .optional();
  }

  @Override
  public List<String> lockForUpdate(String firstId, String secondId) {
    return jdbc.sql("SELECT id FROM wallets WHERE id IN (:a, :b) ORDER BY id FOR UPDATE")
        .param("a", firstId)
        .param("b", secondId)
        .query(String.class)
        .list();
  }

  @Override
  public boolean debitIfSufficient(String id, long amount, Instant now) {
    int updated =
        jdbc.sql(
                """
                UPDATE wallets SET balance = balance - :amount, updated_at = :now
                 WHERE id = :id AND balance >= :amount
                """)
            .param("amount", amount)
            .param("now", Timestamps.toDb(now))
            .param("id", id)
            .update();
    return updated == 1;
  }

  @Override
  public void credit(String id, long amount, Instant now) {
    jdbc.sql("UPDATE wallets SET balance = balance + :amount, updated_at = :now WHERE id = :id")
        .param("amount", amount)
        .param("now", Timestamps.toDb(now))
        .param("id", id)
        .update();
  }
}
