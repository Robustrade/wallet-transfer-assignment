package com.example.wallettransfer.application;

import com.example.wallettransfer.domain.TransferState;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

import java.time.OffsetDateTime;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Concurrency integration test running against a real PostgreSQL container.
 *
 * <p>Verifies that concurrent debit attempts on the same source wallet never
 * result in double spending or a negative balance. The key invariant:
 *
 * <pre>
 *   (processed transfers × transfer amount) + final balance == initial balance
 * </pre>
 *
 * <p>The test also validates that ledger entries are consistent:
 * every processed transfer produces exactly one DEBIT and one CREDIT entry.
 */
@SpringBootTest
@Testcontainers
class ConcurrentTransferIntegrationTest {

    @Container
    static PostgreSQLContainer<?> postgres = new PostgreSQLContainer<>("postgres:16-alpine");

    @DynamicPropertySource
    static void configureDataSource(DynamicPropertyRegistry registry) {
        registry.add("spring.datasource.url", postgres::getJdbcUrl);
        registry.add("spring.datasource.username", postgres::getUsername);
        registry.add("spring.datasource.password", postgres::getPassword);
        registry.add("spring.datasource.driver-class-name", () -> "org.postgresql.Driver");
        registry.add("spring.flyway.enabled", () -> "true");
    }

    @Autowired
    private TransferService transferService;

    @Autowired
    private JdbcTemplate jdbcTemplate;

    private static final long INITIAL_BALANCE = 1_000L;
    private static final long TRANSFER_AMOUNT = 200L;
    private static final int THREAD_COUNT = 8;

    private String sourceWalletId;
    private String targetWalletId;

    @BeforeEach
    void setUp() {
        jdbcTemplate.update("delete from ledger_entries");
        jdbcTemplate.update("delete from transfers");
        jdbcTemplate.update("delete from wallets");

        sourceWalletId = "source-" + UUID.randomUUID();
        targetWalletId = "target-" + UUID.randomUUID();

        OffsetDateTime now = OffsetDateTime.now();
        jdbcTemplate.update(
                "insert into wallets (id, balance, created_at, updated_at) values (?, ?, ?, ?)",
                sourceWalletId, INITIAL_BALANCE, now, now);
        jdbcTemplate.update(
                "insert into wallets (id, balance, created_at, updated_at) values (?, ?, ?, ?)",
                targetWalletId, 0L, now, now);
    }

    @Test
    void concurrentDebitsNeverExceedAvailableFundsAndLedgerBalances() throws Exception {
        // THREAD_COUNT threads each attempt to debit TRANSFER_AMOUNT from the same source.
        // With INITIAL_BALANCE=1000 and TRANSFER_AMOUNT=200 a maximum of 5 can succeed.
        ExecutorService pool = Executors.newFixedThreadPool(THREAD_COUNT);
        CountDownLatch ready = new CountDownLatch(THREAD_COUNT);
        CountDownLatch start = new CountDownLatch(1);

        List<Future<TransferResult>> futures = new ArrayList<>();
        for (int i = 0; i < THREAD_COUNT; i++) {
            String idempotencyKey = "concurrent-key-" + i;
            futures.add(pool.submit(() -> {
                ready.countDown();
                start.await(); // all threads start as simultaneously as possible
                try {
                    return transferService.createTransfer(
                            new CreateTransferCommand(idempotencyKey, sourceWalletId, targetWalletId, TRANSFER_AMOUNT));
                } catch (Exception ex) {
                    // Wallet-not-found or unexpected errors — should not happen here
                    throw new RuntimeException("Transfer failed unexpectedly", ex);
                }
            }));
        }

        ready.await(); // wait for all threads to be ready
        start.countDown(); // release all threads at once

        pool.shutdown();

        List<TransferResult> results = new ArrayList<>();
        for (Future<TransferResult> f : futures) {
            results.add(f.get());
        }

        long processedCount = results.stream()
                .filter(r -> r.state() == TransferState.PROCESSED)
                .count();
        long failedCount = results.stream()
                .filter(r -> r.state() == TransferState.FAILED)
                .count();

        long finalSourceBalance = jdbcTemplate.queryForObject(
                "select balance from wallets where id = ?", Long.class, sourceWalletId);
        long finalTargetBalance = jdbcTemplate.queryForObject(
                "select balance from wallets where id = ?", Long.class, targetWalletId);
        int ledgerRowCount = jdbcTemplate.queryForObject(
                "select count(*) from ledger_entries", Integer.class);

        // Core invariant: no money created or destroyed
        assertThat(processedCount * TRANSFER_AMOUNT + finalSourceBalance)
                .as("total debited + remaining balance must equal initial balance")
                .isEqualTo(INITIAL_BALANCE);

        // Target wallet gained exactly what source wallet lost
        assertThat(finalTargetBalance)
                .as("target balance must equal total credited amount")
                .isEqualTo(processedCount * TRANSFER_AMOUNT);

        // Source balance must never go negative (schema constraint also enforces this)
        assertThat(finalSourceBalance)
                .as("source balance must not be negative")
                .isGreaterThanOrEqualTo(0L);

        // Ledger consistency: exactly 2 entries per processed transfer
        assertThat(ledgerRowCount)
                .as("ledger must have exactly 2 entries per processed transfer")
                .isEqualTo((int) (processedCount * 2));

        // All requests ended in a terminal state
        assertThat(processedCount + failedCount)
                .as("every request must have a terminal result")
                .isEqualTo(THREAD_COUNT);
    }
}
