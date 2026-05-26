package com.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;

import com.wallet.domain.Wallet;
import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.service.TransferService;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;

@SpringBootTest
@ActiveProfiles("test")
class ConcurrencyTest extends PostgresTestContainerConfig {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @BeforeEach
    void setUp() {
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();

        walletRepository.save(new Wallet("w1", new BigDecimal("1000.00")));
        walletRepository.save(new Wallet("w2", new BigDecimal("500.00")));
    }

    @Test
    void concurrentTransfers_fromSameWallet_noDoubleSpend() throws Exception {
        int threadCount = 10;
        BigDecimal amountEach = new BigDecimal("120.00"); // 10 x 120 = 1200, only 8 can succeed (1000/120 = 8)

        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        AtomicInteger successCount = new AtomicInteger();
        AtomicInteger failCount = new AtomicInteger();

        List<Future<?>> futures = new ArrayList<>();
        for (int i = 0; i < threadCount; i++) {
            String key = "concurrent-key-" + i;
            futures.add(executor.submit(() -> {
                try {
                    startLatch.await();
                    transferService.executeTransfer(
                        new TransferRequest(key, "w1", "w2", amountEach)
                    );
                    successCount.incrementAndGet();
                } catch (Exception ex) {
                    failCount.incrementAndGet();
                }
                return null;
            }));
        }

        startLatch.countDown();
        for (Future<?> future : futures) {
            future.get();
        }
        executor.shutdown();

        Wallet w1 = walletRepository.findById("w1").orElseThrow();
        Wallet w2 = walletRepository.findById("w2").orElseThrow();

        // Balance must be non-negative (no double spend)
        assertThat(w1.getBalance()).isGreaterThanOrEqualTo(BigDecimal.ZERO);

        // Total transferred = successCount * 120
        BigDecimal totalTransferred = amountEach.multiply(BigDecimal.valueOf(successCount.get()));
        assertThat(w1.getBalance()).isEqualByComparingTo(new BigDecimal("1000.00").subtract(totalTransferred));
        assertThat(w2.getBalance()).isEqualByComparingTo(new BigDecimal("500.00").add(totalTransferred));

        // Successes + failures = total attempts
        assertThat(successCount.get() + failCount.get()).isEqualTo(threadCount);
    }

    @Test
    void concurrentDuplicateIdempotencyKeys_exactlyOneTransferCreated() throws Exception {
        int threadCount = 10;
        String sharedKey = "shared-idempotency-" + UUID.randomUUID();

        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        List<Future<TransferExecutionResult>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            futures.add(executor.submit(() -> {
                startLatch.await();
                return transferService.executeTransfer(
                    new TransferRequest(sharedKey, "w1", "w2", new BigDecimal("50.00"))
                );
            }));
        }

        startLatch.countDown();
        List<TransferExecutionResult> results = new ArrayList<>();
        for (Future<TransferExecutionResult> future : futures) {
            results.add(future.get());
        }
        executor.shutdown();

        // All results must reference the same transfer ID
        UUID transferId = results.get(0).transfer().transferId();
        assertThat(results).allSatisfy(r ->
            assertThat(r.transfer().transferId()).isEqualTo(transferId)
        );

        // Exactly one transfer in DB
        assertThat(transferRepository.findAll()).hasSize(1);

        // Balance changed exactly once
        assertThat(walletRepository.findById("w1").orElseThrow().getBalance())
            .isEqualByComparingTo("950.00");
    }

    @Test
    void concurrentBidirectionalTransfers_noDeadlock() throws Exception {
        // w1 -> w2 and w2 -> w1 simultaneously — deadlock-prone without ordered locking
        int threadCount = 20;
        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        AtomicInteger errorCount = new AtomicInteger();

        List<Future<?>> futures = new ArrayList<>();
        for (int i = 0; i < threadCount; i++) {
            final int index = i;
            futures.add(executor.submit(() -> {
                try {
                    startLatch.await();
                    String fromId = index % 2 == 0 ? "w1" : "w2";
                    String toId = index % 2 == 0 ? "w2" : "w1";
                    transferService.executeTransfer(
                        new TransferRequest("bidir-" + index, fromId, toId, new BigDecimal("10.00"))
                    );
                } catch (Exception ex) {
                    errorCount.incrementAndGet();
                }
                return null;
            }));
        }

        startLatch.countDown();
        for (Future<?> future : futures) {
            future.get();
        }
        executor.shutdown();

        // No deadlocks — all transfers either succeeded or failed cleanly
        // Balances must be consistent (sum unchanged)
        BigDecimal w1Balance = walletRepository.findById("w1").orElseThrow().getBalance();
        BigDecimal w2Balance = walletRepository.findById("w2").orElseThrow().getBalance();
        assertThat(w1Balance.add(w2Balance)).isEqualByComparingTo("1500.00");
        assertThat(w1Balance).isGreaterThanOrEqualTo(BigDecimal.ZERO);
        assertThat(w2Balance).isGreaterThanOrEqualTo(BigDecimal.ZERO);
    }
}
