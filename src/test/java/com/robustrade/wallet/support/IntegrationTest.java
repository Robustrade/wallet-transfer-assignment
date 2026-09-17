package com.robustrade.wallet.support;

import java.math.BigDecimal;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.springframework.jdbc.core.JdbcTemplate;
import org.testcontainers.containers.PostgreSQLContainer;

/**
 * Base class for tests that need a real Postgres: row locks, ON CONFLICT and the CHECK constraints
 * are the things under test, and none of them behave the same on an in-memory database.
 *
 * <p>The container is a singleton, shared by every subclass and every cached context.
 */
@SpringBootTest
public abstract class IntegrationTest {

  /**
   * Started once for the whole JVM and deliberately never stopped: Spring caches its contexts
   * across test classes, so a container tied to one class's lifecycle would leave later classes
   * pointing at a dead port. Testcontainers' reaper removes it when the build exits.
   */
  @ServiceConnection
  static final PostgreSQLContainer<?> POSTGRES = new PostgreSQLContainer<>("postgres:16-alpine");

  static {
    POSTGRES.start();
  }

  @Autowired protected JdbcTemplate jdbc;

  /**
   * The container is shared, so every test both starts and ends at the seeded state. Restoring on
   * both sides means no test can be broken by the order it happens to run in.
   */
  @BeforeEach
  @AfterEach
  void resetToSeed() {
    jdbc.update("DELETE FROM ledger_entries");
    jdbc.update("DELETE FROM transfers");
    jdbc.update("UPDATE wallets SET balance = 1000 WHERE id = 'wallet_1'");
    jdbc.update("UPDATE wallets SET balance = 500 WHERE id = 'wallet_2'");
    jdbc.update("UPDATE wallets SET balance = 0 WHERE id = 'wallet_3'");
    jdbc.update("UPDATE wallets SET balance = 250 WHERE id = 'wallet_4'");
  }

  protected BigDecimal balanceOf(String walletId) {
    return jdbc.queryForObject(
        "SELECT balance FROM wallets WHERE id = ?", BigDecimal.class, walletId);
  }

  protected int transferCount() {
    return jdbc.queryForObject("SELECT count(*) FROM transfers", Integer.class);
  }

  protected int ledgerEntryCount() {
    return jdbc.queryForObject("SELECT count(*) FROM ledger_entries", Integer.class);
  }

  /** Debits are negative, credits positive. A balanced ledger always sums to zero. */
  protected BigDecimal ledgerNet() {
    return jdbc.queryForObject(
        "SELECT coalesce(sum(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)"
            + " FROM ledger_entries",
        BigDecimal.class);
  }
}
