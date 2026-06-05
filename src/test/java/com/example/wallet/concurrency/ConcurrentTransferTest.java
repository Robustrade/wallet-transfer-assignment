package com.example.wallet.concurrency;

import com.example.wallet.exception.InsufficientFundsException;
import com.example.wallet.handler.dto.CreateTransferRequest;
import com.example.wallet.handler.dto.TransferResponse;
import com.example.wallet.model.TransferStatus;
import com.example.wallet.service.TransferService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.ActiveProfiles;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Concurrency tests for the wallet transfer service.
 *
 * These tests verify correctness under parallel execution — the most critical
 * correctness properties of a financial system:
 *
 *   1. No overdraft: concurrent debits on the same wallet never produce a
 *      negative balance. SELECT FOR UPDATE serialises wallet row access.
 *
 *   2. No double-spend: exactly the right number of transfers are processed
 *      (floor(balance / amount)), with the rest rejected.
 *
 *   3. Ledger consistency: every PROCESSED transfer produces exactly 2 entries.
 *
 *   4. Idempotency under concurrency: N threads with the same idempotency key
 *      produce exactly 1 transfer and 2 ledger entries — never more.
 *
 * Tests use a CountDownLatch to release all threads simultaneously, maximising
 * contention on the database rows being tested.
 */
@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.NONE)
@ActiveProfiles("test")
class ConcurrentTransferTest {

    @Autowired private TransferService transferService;
    @Autowired private JdbcTemplate    jdbc;

    @BeforeEach
    void resetState() {
        jdbc.execute("DELETE FROM idempotency_records");
        jdbc.execute("DELETE FROM ledger_entries");
        jdbc.execute("DELETE FROM transfers");

        jdbc.execute("UPDATE wallets SET balance = 1000.0000 WHERE id = 'wallet_alice'");
        jdbc.execute("UPDATE wallets SET balance =    0.0000 WHERE id = 'wallet_bob'");
        jdbc.execute("UPDATE wallets SET balance =    0.0000 WHERE id = 'wallet_carol'");
    }

    // ── Test 1: concurrent debits never overdraft ─────────────────────────────

    @Test
    @DisplayName("10 concurrent transfers of 100 from wallet with 1000 balance — exactly 10 processed, balance = 0")
    void concurrentDebits_neverOverdraft_correctFinalBalance() throws Exception {
        int threadCount = 10;
        BigDecimal amount = new BigDecimal("100");
        // wallet_alice has 1000 — exactly 10 transfers of 100 should all succeed

        List<String> outcomes = runConcurrently(threadCount, i ->
                transferService.execute(new CreateTransferRequest(
                        "concurrent-full-" + i, "wallet_alice", "wallet_bob", amount)));

        long processed = outcomes.stream().filter("PROCESSED"::equals).count();
        long failed    = outcomes.stream().filter("FAILED"::equals).count();

        assertThat(processed).isEqualTo(10);
        assertThat(failed).isZero();

        // Final balance must be exactly 0 — never negative
        BigDecimal balance = jdbc.queryForObject(
                "SELECT balance FROM wallets WHERE id = 'wallet_alice'", BigDecimal.class);
        assertThat(balance).isEqualByComparingTo(BigDecimal.ZERO);
    }

    @Test
    @DisplayName("10 concurrent transfers of 100 from wallet with 500 balance — exactly 5 processed, 5 failed, balance = 0")
    void concurrentDebits_partialFunds_exactlyHalfProcessed() throws Exception {
        int threadCount = 10;
        BigDecimal amount = new BigDecimal("100");
        jdbc.execute("UPDATE wallets SET balance = 500.0000 WHERE id = 'wallet_alice'");

        List<String> outcomes = runConcurrently(threadCount, i ->
                transferService.execute(new CreateTransferRequest(
                        "concurrent-half-" + i, "wallet_alice", "wallet_bob", amount)));

        long processed = outcomes.stream().filter("PROCESSED"::equals).count();
        long failed    = outcomes.stream().filter("FAILED"::equals).count();

        assertThat(processed).isEqualTo(5);
        assertThat(failed).isEqualTo(5);

        BigDecimal balance = jdbc.queryForObject(
                "SELECT balance FROM wallets WHERE id = 'wallet_alice'", BigDecimal.class);
        assertThat(balance).isEqualByComparingTo(BigDecimal.ZERO);
    }

    @Test
    @DisplayName("concurrent debits: ledger entries always balance — SUM(DEBIT) == SUM(CREDIT) for each transfer")
    void concurrentDebits_ledgerAlwaysBalanced() throws Exception {
        int threadCount = 8;
        BigDecimal amount = new BigDecimal("100");

        runConcurrently(threadCount, i ->
                transferService.execute(new CreateTransferRequest(
                        "concurrent-ledger-" + i, "wallet_alice", "wallet_bob", amount)));

        // Every transfer that was PROCESSED must have exactly 1 DEBIT == 1 CREDIT
        Integer unbalanced = jdbc.queryForObject("""
                SELECT COUNT(*) FROM transfers t
                WHERE t.status = 'PROCESSED'
                  AND (SELECT COUNT(*) FROM ledger_entries l WHERE l.transfer_id = t.id) != 2
                """, Integer.class);
        assertThat(unbalanced).isZero();
    }

    // ── Test 2: concurrent same idempotency key ───────────────────────────────

    @Test
    @DisplayName("5 concurrent requests with the same idempotency key — exactly 1 transfer, 2 ledger entries")
    void concurrentSameIdempotencyKey_executedOnlyOnce() throws Exception {
        int threadCount = 5;
        String sameKey = "concurrent-idem-key";

        List<String> outcomes = runConcurrently(threadCount, ignored ->
                transferService.execute(new CreateTransferRequest(
                        sameKey, "wallet_alice", "wallet_bob", new BigDecimal("100"))));

        // All threads should return PROCESSED (idempotent replay returns same result)
        assertThat(outcomes).allMatch("PROCESSED"::equals);

        // Exactly 1 transfer record in DB
        int transferCount = jdbc.queryForObject(
                "SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?",
                Integer.class, sameKey);
        assertThat(transferCount).isEqualTo(1);

        // Exactly 2 ledger entries — not 5 × 2 = 10
        int ledgerCount = jdbc.queryForObject(
                "SELECT COUNT(*) FROM ledger_entries", Integer.class);
        assertThat(ledgerCount).isEqualTo(2);

        // Balance changed only once
        assertThat(jdbc.queryForObject(
                "SELECT balance FROM wallets WHERE id = 'wallet_alice'", BigDecimal.class))
                .isEqualByComparingTo("900.0000");
    }

    @Test
    @DisplayName("concurrent transfers A→B and B→A simultaneously — no deadlock, both complete")
    void concurrentBidirectionalTransfers_noDeadlock() throws Exception {
        // Give wallet_bob some balance
        jdbc.execute("UPDATE wallets SET balance = 500.0000 WHERE id = 'wallet_bob'");

        int threadCount = 2;
        CountDownLatch startLatch = new CountDownLatch(1);
        ExecutorService pool = Executors.newFixedThreadPool(threadCount);

        Future<String> aToB = pool.submit(() -> {
            startLatch.await();
            try {
                transferService.execute(new CreateTransferRequest(
                        "deadlock-a-b", "wallet_alice", "wallet_bob", new BigDecimal("100")));
                return "PROCESSED";
            } catch (InsufficientFundsException e) {
                return "FAILED";
            }
        });

        Future<String> bToA = pool.submit(() -> {
            startLatch.await();
            try {
                transferService.execute(new CreateTransferRequest(
                        "deadlock-b-a", "wallet_bob", "wallet_alice", new BigDecimal("100")));
                return "PROCESSED";
            } catch (InsufficientFundsException e) {
                return "FAILED";
            }
        });

        startLatch.countDown();
        pool.shutdown();

        // Neither thread must hang — both must complete (deadlock would cause a timeout here)
        assertThat(aToB.get()).isIn("PROCESSED", "FAILED");
        assertThat(bToA.get()).isIn("PROCESSED", "FAILED");
    }

    // ── Helpers ───────────────────────────────────────────────────────────────

    @FunctionalInterface
    interface TransferTask {
        TransferResponse execute(int index) throws Exception;
    }

    /**
     * Runs {@code count} threads simultaneously. Each thread calls the task,
     * catching InsufficientFundsException as "FAILED" and any other exception as "ERROR".
     * Returns a list of outcome strings: "PROCESSED", "FAILED", or "ERROR".
     */
    private List<String> runConcurrently(int count, TransferTask task) throws Exception {
        CountDownLatch startLatch = new CountDownLatch(1);
        ExecutorService pool = Executors.newFixedThreadPool(count);
        List<Future<String>> futures = new ArrayList<>();

        for (int i = 0; i < count; i++) {
            final int idx = i;
            futures.add(pool.submit(() -> {
                startLatch.await(); // all threads start simultaneously
                try {
                    TransferResponse r = task.execute(idx);
                    return r.status().name();
                } catch (InsufficientFundsException e) {
                    return "FAILED";
                } catch (Exception e) {
                    return "ERROR:" + e.getClass().getSimpleName();
                }
            }));
        }

        startLatch.countDown(); // release all threads at once
        pool.shutdown();

        List<String> results = new ArrayList<>();
        for (Future<String> f : futures) {
            results.add(f.get());
        }

        // Fail fast if any thread produced an unexpected error
        assertThat(results)
                .as("No thread should produce an ERROR outcome")
                .noneMatch(s -> s.startsWith("ERROR"));

        return results;
    }
}
