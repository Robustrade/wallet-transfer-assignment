package com.wallettransfer.support;

import org.junit.jupiter.api.BeforeEach;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;

/**
 * Base class for tests that need the real database. One PostgreSQL container is started for the
 * whole test JVM (singleton container pattern) and the tables are emptied before every test.
 */
@SpringBootTest
public abstract class PostgresIntegrationTest {

  @SuppressWarnings("resource") // lives for the whole JVM and is reaped by Testcontainers' Ryuk
  private static final PostgreSQLContainer<?> POSTGRES =
      new PostgreSQLContainer<>("postgres:16-alpine");

  static {
    POSTGRES.start();
  }

  @Autowired protected JdbcTemplate jdbc;

  @DynamicPropertySource
  static void databaseProperties(DynamicPropertyRegistry registry) {
    registry.add("spring.datasource.url", POSTGRES::getJdbcUrl);
    registry.add("spring.datasource.username", POSTGRES::getUsername);
    registry.add("spring.datasource.password", POSTGRES::getPassword);
  }

  @BeforeEach
  void cleanDatabase() {
    jdbc.execute(
        "TRUNCATE ledger_entries, idempotency_records, transfers, wallets"
            + " RESTART IDENTITY CASCADE");
  }
}
