package com.wallet.integration;

import com.wallet.controller.dto.CreateTransferRequest;
import com.wallet.controller.dto.TransferResponse;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for concurrency safety.
 * Verifies that the system handles concurrent transfers correctly.
 */
class ConcurrencyIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TestRestTemplate restTemplate;

    @Test
    @DisplayName("Should handle concurrent transfers from the same wallet without overdraft")
    void shouldPreventOverdraftUnderConcurrency() throws Exception {
        // wallet_3 has 500000 cents ($5000)
        // Send 10 concurrent transfers of 100000 each (total 1000000 = $10000)
        // Only 5 should succeed, the rest should fail with insufficient balance
        int threadCount = 10;
        long amountPerTransfer = 100000L;

        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        List<Future<ResponseEntity<TransferResponse>>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            final int index = i;
            futures.add(executor.submit(() -> {
                startLatch.await(); // All threads start simultaneously
                CreateTransferRequest request = new CreateTransferRequest("wallet_3", "wallet_2", amountPerTransfer);
                return postTransfer(
                        restTemplate,
                        "concurrent-overdraft-" + index + "-" + UUID.randomUUID(),
                        request,
                        TransferResponse.class
                );
            }));
        }

        // Release all threads at once
        startLatch.countDown();

        // Collect results
        AtomicInteger successCount = new AtomicInteger(0);
        AtomicInteger failCount = new AtomicInteger(0);

        for (Future<ResponseEntity<TransferResponse>> future : futures) {
            ResponseEntity<TransferResponse> response = future.get();
            if (response.getStatusCode() == HttpStatus.CREATED) {
                successCount.incrementAndGet();
            } else {
                failCount.incrementAndGet();
            }
        }

        executor.shutdown();

        // wallet_3 starts each test at 500_000 (BaseIntegrationTest cleans + remigrates).
        // 10 concurrent transfers of 100_000 should result in exactly 5 successes,
        // 5 insufficient-balance failures, and a final balance of exactly 0.
        var balanceResponse = getWithClient(restTemplate, "/wallets/wallet_3/balance", Map.class).getBody();
        long finalBalance = ((Number) balanceResponse.get("balance")).longValue();

        assertThat(successCount.get() + failCount.get()).isEqualTo(threadCount);
        assertThat(successCount.get()).isEqualTo(5);
        assertThat(failCount.get()).isEqualTo(5);
        assertThat(finalBalance).isZero();
    }

    @Test
    @DisplayName("Should handle concurrent bidirectional transfers without deadlock")
    void shouldHandleBidirectionalTransfersWithoutDeadlock() throws Exception {
        // Simultaneously transfer A→B and B→A
        // The ordered locking strategy should prevent deadlocks
        int threadCount = 20;

        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        List<Future<ResponseEntity<?>>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            final int index = i;
            futures.add(executor.submit(() -> {
                startLatch.await();
                CreateTransferRequest request;
                String idempotencyKey = "bidir-" + index + "-" + UUID.randomUUID();
                if (index % 2 == 0) {
                    // wallet_1 → wallet_2
                    request = new CreateTransferRequest("wallet_1", "wallet_2", 10L);
                } else {
                    // wallet_2 → wallet_1
                    request = new CreateTransferRequest("wallet_2", "wallet_1", 10L);
                }
                return postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);
            }));
        }

        startLatch.countDown();

        // All should complete without deadlock (timeout would indicate deadlock)
        int completed = 0;
        for (Future<ResponseEntity<?>> future : futures) {
            future.get(); // Will throw if deadlocked (timeout)
            completed++;
        }

        executor.shutdown();

        assertThat(completed).isEqualTo(threadCount);

        // Verify both wallets still have non-negative balances
        var balance1 = getWithClient(restTemplate, "/wallets/wallet_1/balance", Map.class).getBody();
        var balance2 = getWithClient(restTemplate, "/wallets/wallet_2/balance", Map.class).getBody();

        assertThat(((Number) balance1.get("balance")).longValue()).isGreaterThanOrEqualTo(0);
        assertThat(((Number) balance2.get("balance")).longValue()).isGreaterThanOrEqualTo(0);
    }

    @Test
    @DisplayName("Should handle concurrent duplicate requests correctly")
    void shouldHandleConcurrentDuplicateRequests() throws Exception {
        // Send the same idempotent request concurrently
        String idempotencyKey = "concurrent-idem-" + UUID.randomUUID();
        int threadCount = 5;

        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch startLatch = new CountDownLatch(1);
        List<Future<ResponseEntity<TransferResponse>>> futures = new ArrayList<>();

        for (int i = 0; i < threadCount; i++) {
            futures.add(executor.submit(() -> {
                startLatch.await();
                CreateTransferRequest request = new CreateTransferRequest("wallet_1", "wallet_2", 5L);
                return postTransfer(restTemplate, idempotencyKey, request, TransferResponse.class);
            }));
        }

        startLatch.countDown();

        // Collect results — all should either succeed or return cached result
        List<ResponseEntity<TransferResponse>> results = new ArrayList<>();
        for (Future<ResponseEntity<TransferResponse>> future : futures) {
            results.add(future.get());
        }

        executor.shutdown();

        // Strict outcome contract: every response is one of
        //   201 CREATED   — the single winner that actually executed the transfer
        //   200 OK        — a replay served from the cached idempotency record
        //   409 CONFLICT  — a sibling that found the row IN_PROGRESS and was rejected
        // Any 5xx — including a leak from markCompleted — should fail this test loudly.
        long createdCount = results.stream()
                .filter(r -> r.getStatusCode() == HttpStatus.CREATED)
                .count();
        long replayCount = results.stream()
                .filter(r -> r.getStatusCode() == HttpStatus.OK)
                .count();
        long conflictCount = results.stream()
                .filter(r -> r.getStatusCode() == HttpStatus.CONFLICT)
                .count();

        assertThat(createdCount)
                .as("exactly one request must win with 201 CREATED")
                .isEqualTo(1);
        assertThat(createdCount + replayCount + conflictCount)
                .as("every response must be 201, 200, or 409 — no 5xx leak")
                .isEqualTo(threadCount);

        // Every 2xx response must reference the same transfer id (CREATED winner
        // and any replays that observed the COMPLETED row).
        List<UUID> transferIds = results.stream()
                .filter(r -> r.getStatusCode().is2xxSuccessful())
                .filter(r -> r.getBody() != null && r.getBody().id() != null)
                .map(r -> r.getBody().id())
                .distinct()
                .toList();
        assertThat(transferIds).hasSize(1);
    }
}
