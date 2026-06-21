package com.wallet.transfer.service;

import com.wallet.transfer.BaseIntegrationTest;
import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;

import com.wallet.transfer.exception.InsufficientFundsException;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;

@DisplayName("Concurrency Safety")
class ConcurrencyTest extends BaseIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private JdbcTemplate jdbcTemplate;

    @BeforeEach
    void setUp() {
        jdbcTemplate.execute("TRUNCATE TABLE ledger_entries, transfers, wallets CASCADE");
    }

    @Test
    @DisplayName("concurrent transfers from same wallet should not allow double spending")
    void noDoubleSpend() throws Exception {
        walletRepository.save(new Wallet("source", 1000L));
        walletRepository.save(new Wallet("dest_1", 0L));
        walletRepository.save(new Wallet("dest_2", 0L));

        int threadCount = 10;
        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch latch = new CountDownLatch(1);
        AtomicInteger successCount = new AtomicInteger(0);
        AtomicInteger failCount = new AtomicInteger(0);

        List<Future<?>> futures = new ArrayList<>();

        // 10 threads each trying to transfer 200 from a wallet with 1000
        // Only 5 should succeed (5 * 200 = 1000)
        for (int i = 0; i < threadCount; i++) {
            final int idx = i;
            String dest = idx % 2 == 0 ? "dest_1" : "dest_2";
            futures.add(executor.submit(() -> {
                try {
                    latch.await();
                    transferService.executeTransfer(
                            "concurrent-" + idx, "source", dest, 200L);
                    successCount.incrementAndGet();
                } catch (Exception e) {
                    failCount.incrementAndGet();
                }
            }));
        }

        latch.countDown(); // release all threads simultaneously
        for (Future<?> f : futures) {
            f.get();
        }
        executor.shutdown();
        assertThat(executor.awaitTermination(10, TimeUnit.SECONDS)).isTrue();

        // Primary safety invariant: balance must never go negative
        Wallet source = walletRepository.findById("source").orElseThrow();
        assertThat(source.getBalance()).isGreaterThanOrEqualTo(0L);

        // With pessimistic locking, exactly 5 succeed (1000 / 200 = 5)
        assertThat(successCount.get()).isEqualTo(5);
        assertThat(failCount.get()).isEqualTo(5);
        assertThat(source.getBalance()).isEqualTo(0L);

        // Money is conserved across the system
        Wallet d1 = walletRepository.findById("dest_1").orElseThrow();
        Wallet d2 = walletRepository.findById("dest_2").orElseThrow();
        assertThat(source.getBalance() + d1.getBalance() + d2.getBalance()).isEqualTo(1000L);
    }

    @Test
    @DisplayName("concurrent transfers between same pair should maintain consistency")
    void bidirectionalConsistency() throws Exception {
        walletRepository.save(new Wallet("alice", 500L));
        walletRepository.save(new Wallet("bob", 500L));

        int transferCount = 20;
        ExecutorService executor = Executors.newFixedThreadPool(10);
        CountDownLatch latch = new CountDownLatch(1);
        List<Future<?>> futures = new ArrayList<>();
        List<Exception> unexpectedErrors = Collections.synchronizedList(new ArrayList<>());

        // Half go alice->bob, half go bob->alice, all for 10 each
        for (int i = 0; i < transferCount; i++) {
            final int idx = i;
            futures.add(executor.submit(() -> {
                try {
                    latch.await();
                    if (idx % 2 == 0) {
                        transferService.executeTransfer(
                                "bidir-" + idx, "alice", "bob", 10L);
                    } else {
                        transferService.executeTransfer(
                                "bidir-" + idx, "bob", "alice", 10L);
                    }
                } catch (InsufficientFundsException expected) {
                    // Expected when a wallet's balance is exhausted
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                } catch (Exception e) {
                    unexpectedErrors.add(e);
                }
            }));
        }

        latch.countDown();
        for (Future<?> f : futures) {
            f.get();
        }
        executor.shutdown();
        assertThat(executor.awaitTermination(10, TimeUnit.SECONDS)).isTrue();

        // No unexpected exceptions should have occurred
        assertThat(unexpectedErrors).as("Only InsufficientFundsException is expected").isEmpty();

        // Total balance in system must remain 1000 (conservation of money)
        Wallet alice = walletRepository.findById("alice").orElseThrow();
        Wallet bob = walletRepository.findById("bob").orElseThrow();
        assertThat(alice.getBalance() + bob.getBalance()).isEqualTo(1000L);

        // Ledger must balance
        List<LedgerEntry> allEntries = ledgerEntryRepository.findAll();
        long totalDebits = allEntries.stream()
                .filter(e -> e.getEntryType() == LedgerEntryType.DEBIT)
                .mapToLong(LedgerEntry::getAmount).sum();
        long totalCredits = allEntries.stream()
                .filter(e -> e.getEntryType() == LedgerEntryType.CREDIT)
                .mapToLong(LedgerEntry::getAmount).sum();
        assertThat(totalDebits).isEqualTo(totalCredits);
    }

    @Test
    @DisplayName("same idempotency key submitted concurrently should result in single transfer")
    void concurrentIdempotency() throws Exception {
        walletRepository.save(new Wallet("w1", 1000L));
        walletRepository.save(new Wallet("w2", 0L));

        int threadCount = 5;
        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch latch = new CountDownLatch(1);
        AtomicInteger successCount = new AtomicInteger(0);
        AtomicInteger failCount = new AtomicInteger(0);

        List<Future<?>> futures = new ArrayList<>();

        // All threads use the same idempotency key
        for (int i = 0; i < threadCount; i++) {
            futures.add(executor.submit(() -> {
                try {
                    latch.await();
                    transferService.executeTransfer("same-key", "w1", "w2", 100L);
                    successCount.incrementAndGet();
                } catch (Exception e) {
                    failCount.incrementAndGet();
                }
            }));
        }

        latch.countDown();
        for (Future<?> f : futures) {
            f.get();
        }
        executor.shutdown();
        assertThat(executor.awaitTermination(10, TimeUnit.SECONDS)).isTrue();

        // All threads should succeed — either the winner executes the transfer,
        // or the losers gracefully recover via the UNIQUE constraint catch + re-lookup.
        // If any thread fails, the idempotency race recovery is broken.
        assertThat(successCount.get()).as("All threads should succeed (execute or idempotent return)")
                .isEqualTo(threadCount);
        assertThat(failCount.get()).as("No thread should fail").isEqualTo(0);

        // Only one debit should have occurred regardless of thread count
        Wallet source = walletRepository.findById("w1").orElseThrow();
        assertThat(source.getBalance()).isEqualTo(900L);

        // Verify only one transfer record in the database
        long transferRecordCount = transferRepository.count();
        assertThat(transferRecordCount).isEqualTo(1L);
    }
}
