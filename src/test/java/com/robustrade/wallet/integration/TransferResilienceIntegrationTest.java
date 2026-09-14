package com.robustrade.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.status;

import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.handler.dto.StatementResponse;
import com.robustrade.wallet.handler.dto.TransferResponse;
import com.robustrade.wallet.handler.dto.WalletResponse;
import com.robustrade.wallet.repository.TransferRepository;
import com.robustrade.wallet.service.TransferStatusService;
import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.stream.Collectors;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.test.web.servlet.MvcResult;
import org.springframework.transaction.support.TransactionTemplate;

/**
 * Extra integration coverage for concurrency, state-machine safety, retry behavior,
 * and balance conservation.
 */
class TransferResilienceIntegrationTest extends AbstractIntegrationTest {

    @Autowired
    private TransferStatusService transferStatusService;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private TransactionTemplate transactionTemplate;

    // --- Concurrency ---

    @Test
    void concurrentIdenticalIdempotencyKey_createsSingleTransferAndDebitsOnce() throws Exception {
        WalletResponse from = createWallet(5001L, "200.00");
        WalletResponse to = createWallet(5002L, "0.00");
        String key = "concurrent-same-key";

        ExecutorService pool = Executors.newFixedThreadPool(8);
        List<TransferResponse> successes = new ArrayList<>();
        try {
            List<Callable<Void>> tasks = new ArrayList<>();
            for (int i = 0; i < 12; i++) {
                tasks.add(() -> {
                    MvcResult result = performTransfer(key, from.getWalletId(), to.getWalletId(), "50.00")
                            .andExpect(status().isCreated())
                            .andReturn();
                    synchronized (successes) {
                        successes.add(objectMapper.readValue(
                                result.getResponse().getContentAsString(),
                                TransferResponse.class
                        ));
                    }
                    return null;
                });
            }
            for (Future<Void> future : pool.invokeAll(tasks)) {
                future.get(30, TimeUnit.SECONDS);
            }
        } finally {
            pool.shutdownNow();
        }

        assertThat(successes).hasSize(12);
        UUID transferId = successes.get(0).getTransferId();
        assertThat(successes).allMatch(r -> transferId.equals(r.getTransferId()));
        assertThat(successes).allMatch(r -> r.getState() == TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("150.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("50.00");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(2);
        Integer transferRows = jdbcTemplate.queryForObject(
                "SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?",
                Integer.class,
                key
        );
        assertThat(transferRows).isEqualTo(1);
    }

    @Test
    void concurrentOppositeDirectionTransfers_conserveTotalBalance() throws Exception {
        WalletResponse a = createWallet(5003L, "500.00");
        WalletResponse b = createWallet(5004L, "500.00");
        BigDecimal totalBefore = walletBalance(a.getWalletId()).add(walletBalance(b.getWalletId()));

        ExecutorService pool = Executors.newFixedThreadPool(6);
        AtomicInteger success = new AtomicInteger();
        try {
            List<Callable<Void>> tasks = new ArrayList<>();
            for (int i = 0; i < 10; i++) {
                final int index = i;
                tasks.add(() -> {
                    MvcResult ab = performTransfer(
                            "opp-a-b-" + index,
                            a.getWalletId(),
                            b.getWalletId(),
                            "10.00"
                    ).andReturn();
                    MvcResult ba = performTransfer(
                            "opp-b-a-" + index,
                            b.getWalletId(),
                            a.getWalletId(),
                            "10.00"
                    ).andReturn();
                    if (ab.getResponse().getStatus() == 201) {
                        success.incrementAndGet();
                    }
                    if (ba.getResponse().getStatus() == 201) {
                        success.incrementAndGet();
                    }
                    return null;
                });
            }
            for (Future<Void> future : pool.invokeAll(tasks)) {
                future.get(60, TimeUnit.SECONDS);
            }
        } finally {
            pool.shutdownNow();
        }

        assertThat(success.get()).isEqualTo(20);
        BigDecimal totalAfter = walletBalance(a.getWalletId()).add(walletBalance(b.getWalletId()));
        assertThat(totalAfter).isEqualByComparingTo(totalBefore);
        assertThat(walletBalance(a.getWalletId()).compareTo(BigDecimal.ZERO)).isGreaterThanOrEqualTo(0);
        assertThat(walletBalance(b.getWalletId()).compareTo(BigDecimal.ZERO)).isGreaterThanOrEqualTo(0);
    }

    @Test
    void markFailed_afterProcessed_doesNotOverwriteStateOrLedger() throws Exception {
        WalletResponse from = createWallet(5050L, "100.00");
        WalletResponse to = createWallet(5051L, "0.00");
        TransferResponse done = createTransfer(
                "mark-failed-after-processed",
                from.getWalletId(),
                to.getWalletId(),
                "25.00"
        );
        assertThat(done.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(ledgerEntryCount(done.getTransferId())).isEqualTo(2);

        transferStatusService.markFailed(done.getTransferId(), "stale failure after success");

        assertThat(transferState(done.getTransferId())).isEqualTo("PROCESSED");
        assertThat(ledgerEntryCount(done.getTransferId())).isEqualTo(2);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("75.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("25.00");
    }

    @Test
    void markFailed_blockedBehindSuccessLock_doesNotOverwriteProcessed() throws Exception {
        WalletResponse from = createWallet(5052L, "100.00");
        WalletResponse to = createWallet(5053L, "0.00");
        Transfer pending = Transfer.createPending(
                "mark-failed-race",
                from.getWalletId(),
                to.getWalletId(),
                new BigDecimal("10.00")
        );
        transferRepository.save(pending);
        UUID transferId = pending.getId();

        CountDownLatch lockHeld = new CountDownLatch(1);
        CountDownLatch commitSuccess = new CountDownLatch(1);
        ExecutorService pool = Executors.newFixedThreadPool(2);
        try {
            Future<?> successPath = pool.submit(() -> {
                transactionTemplate.executeWithoutResult(status -> {
                    Transfer locked = transferRepository.findByIdForUpdate(transferId).orElseThrow();
                    lockHeld.countDown();
                    try {
                        if (!commitSuccess.await(15, TimeUnit.SECONDS)) {
                            throw new IllegalStateException("Timed out waiting to commit success");
                        }
                    } catch (InterruptedException e) {
                        Thread.currentThread().interrupt();
                        throw new IllegalStateException(e);
                    }
                    locked.markProcessed();
                    transferRepository.save(locked);
                });
            });

            if (!lockHeld.await(15, TimeUnit.SECONDS)) {
                throw new IllegalStateException("Timed out waiting for success path lock");
            }

            Future<?> markFailedPath = pool.submit(() ->
                    transferStatusService.markFailed(transferId, "concurrent stale failure")
            );
            // Allow markFailed to block on the same row lock before success commits.
            Thread.sleep(150);
            commitSuccess.countDown();

            successPath.get(15, TimeUnit.SECONDS);
            markFailedPath.get(15, TimeUnit.SECONDS);
        } finally {
            pool.shutdownNow();
        }

        assertThat(transferState(transferId)).isEqualTo("PROCESSED");
    }

    // --- Safe state transitions ---

    @Test
    void processedTransfer_replayKeepsProcessedAndDoesNotChangeStateOrBalances() throws Exception {
        WalletResponse from = createWallet(5101L, "300.00");
        WalletResponse to = createWallet(5102L, "0.00");

        TransferResponse first = createTransfer("state-success-1", from.getWalletId(), to.getWalletId(), "40.00");
        assertThat(first.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(transferState(first.getTransferId())).isEqualTo("PROCESSED");

        TransferResponse replay = createTransfer("state-success-1", from.getWalletId(), to.getWalletId(), "40.00");
        assertThat(replay.getTransferId()).isEqualTo(first.getTransferId());
        assertThat(replay.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(transferState(first.getTransferId())).isEqualTo("PROCESSED");
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("260.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("40.00");
        assertThat(ledgerEntryCount(first.getTransferId())).isEqualTo(2);
    }

    @Test
    void failedTransfer_remainsFailedUntilSuccessfulRetry() throws Exception {
        WalletResponse from = createWallet(5103L, "20.00");
        WalletResponse to = createWallet(5104L, "0.00");

        MvcResult failed = performTransfer("state-failed-1", from.getWalletId(), to.getWalletId(), "100.00")
                .andReturn();
        assertThat(failed.getResponse().getStatus()).isEqualTo(400);
        assertThat(errorCode(failed)).isEqualTo("INSUFFICIENT_BALANCE");

        UUID transferId = jdbcTemplate.queryForObject(
                "SELECT transfer_id FROM idempotency_records WHERE idempotency_key = ?",
                UUID.class,
                "state-failed-1"
        );
        assertThat(transferState(transferId)).isEqualTo("FAILED");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(0);

        // Same payload again without top-up: still FAILED (retry attempted, fails again)
        MvcResult stillFailed = performTransfer("state-failed-1", from.getWalletId(), to.getWalletId(), "100.00")
                .andReturn();
        assertThat(stillFailed.getResponse().getStatus()).isEqualTo(400);
        assertThat(errorCode(stillFailed)).isEqualTo("INSUFFICIENT_BALANCE");
        assertThat(transferState(transferId)).isEqualTo("FAILED");
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("20.00");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(0);
    }

    // --- Retry-safe behavior ---

    @Test
    void retryAfterTopUp_isIdempotentOnFurtherReplays() throws Exception {
        WalletResponse from = createWallet(5201L, "30.00");
        WalletResponse to = createWallet(5202L, "0.00");
        String key = "retry-safe-1";

        performTransfer(key, from.getWalletId(), to.getWalletId(), "100.00")
                .andExpect(status().isBadRequest());

        jdbcTemplate.update(
                "UPDATE wallets SET balance = ? WHERE id = ?",
                new BigDecimal("500.00"),
                from.getWalletId()
        );

        TransferResponse retried = createTransfer(key, from.getWalletId(), to.getWalletId(), "100.00");
        assertThat(retried.getState()).isEqualTo(TransferState.PROCESSED);
        UUID transferId = retried.getTransferId();
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("400.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("100.00");

        TransferResponse replay = createTransfer(key, from.getWalletId(), to.getWalletId(), "100.00");
        assertThat(replay.getTransferId()).isEqualTo(transferId);
        assertThat(replay.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("400.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("100.00");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(2);
    }

    @Test
    void concurrentRetriesOfFailedKey_afterTopUp_succeedOnce() throws Exception {
        WalletResponse from = createWallet(5203L, "10.00");
        WalletResponse to = createWallet(5204L, "0.00");
        String key = "retry-concurrent-1";

        performTransfer(key, from.getWalletId(), to.getWalletId(), "80.00")
                .andExpect(status().isBadRequest());

        jdbcTemplate.update(
                "UPDATE wallets SET balance = ? WHERE id = ?",
                new BigDecimal("200.00"),
                from.getWalletId()
        );

        ExecutorService pool = Executors.newFixedThreadPool(6);
        List<TransferResponse> responses = new ArrayList<>();
        try {
            List<Callable<MvcResult>> tasks = new ArrayList<>();
            for (int i = 0; i < 8; i++) {
                tasks.add(() -> performTransfer(key, from.getWalletId(), to.getWalletId(), "80.00").andReturn());
            }
            for (Future<MvcResult> future : pool.invokeAll(tasks)) {
                MvcResult result = future.get(30, TimeUnit.SECONDS);
                assertThat(result.getResponse().getStatus()).isEqualTo(201);
                responses.add(objectMapper.readValue(
                        result.getResponse().getContentAsString(),
                        TransferResponse.class
                ));
            }
        } finally {
            pool.shutdownNow();
        }

        UUID transferId = responses.get(0).getTransferId();
        assertThat(responses.stream().map(TransferResponse::getTransferId).collect(Collectors.toSet()))
                .containsExactly(transferId);
        assertThat(responses).allMatch(r -> r.getState() == TransferState.PROCESSED);
        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("120.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("80.00");
        assertThat(ledgerEntryCount(transferId)).isEqualTo(2);
    }

    // --- Correct balance tracking ---

    @Test
    void multiHopTransfers_statementBalancesMatchWalletAndConserveFunds() throws Exception {
        WalletResponse a = createWallet(5301L, "1000.00");
        WalletResponse b = createWallet(5302L, "100.00");
        WalletResponse c = createWallet(5303L, "50.00");
        BigDecimal totalBefore = walletBalance(a.getWalletId())
                .add(walletBalance(b.getWalletId()))
                .add(walletBalance(c.getWalletId()));

        createTransfer("bal-a-b", a.getWalletId(), b.getWalletId(), "200.00");
        createTransfer("bal-b-c", b.getWalletId(), c.getWalletId(), "75.00");
        createTransfer("bal-c-a", c.getWalletId(), a.getWalletId(), "25.00");

        assertThat(walletBalance(a.getWalletId())).isEqualByComparingTo("825.00");
        assertThat(walletBalance(b.getWalletId())).isEqualByComparingTo("225.00");
        assertThat(walletBalance(c.getWalletId())).isEqualByComparingTo("100.00");

        BigDecimal totalAfter = walletBalance(a.getWalletId())
                .add(walletBalance(b.getWalletId()))
                .add(walletBalance(c.getWalletId()));
        assertThat(totalAfter).isEqualByComparingTo(totalBefore);
        assertThat(totalAfter).isEqualByComparingTo("1150.00");

        StatementResponse stmtA = getStatementByWalletId(a.getWalletId());
        StatementResponse stmtB = getStatementByWalletId(b.getWalletId());
        StatementResponse stmtC = getStatementByWalletId(c.getWalletId());

        assertThat(stmtA.getCurrentBalance()).isEqualByComparingTo(walletBalance(a.getWalletId()));
        assertThat(stmtB.getCurrentBalance()).isEqualByComparingTo(walletBalance(b.getWalletId()));
        assertThat(stmtC.getCurrentBalance()).isEqualByComparingTo(walletBalance(c.getWalletId()));

        assertThat(stmtA.getEntries()).hasSize(3);
        assertThat(stmtA.getEntries().get(0).getBalanceAfterTransfer()).isEqualByComparingTo("1000.00");
        assertThat(stmtA.getEntries().get(1).getBalanceAfterTransfer()).isEqualByComparingTo("800.00");
        assertThat(stmtA.getEntries().get(2).getBalanceAfterTransfer()).isEqualByComparingTo("825.00");

        assertThat(stmtB.getEntries()).hasSize(3);
        assertThat(stmtB.getEntries().get(0).getBalanceAfterTransfer()).isEqualByComparingTo("100.00");
        assertThat(stmtB.getEntries().get(1).getBalanceAfterTransfer()).isEqualByComparingTo("300.00");
        assertThat(stmtB.getEntries().get(2).getBalanceAfterTransfer()).isEqualByComparingTo("225.00");

        assertThat(stmtC.getEntries()).hasSize(3);
        assertThat(stmtC.getEntries().get(0).getBalanceAfterTransfer()).isEqualByComparingTo("50.00");
        assertThat(stmtC.getEntries().get(1).getBalanceAfterTransfer()).isEqualByComparingTo("125.00");
        assertThat(stmtC.getEntries().get(2).getBalanceAfterTransfer()).isEqualByComparingTo("100.00");
    }

    @Test
    void failedTransfer_doesNotAffectBalancesOrStatementEntries() throws Exception {
        WalletResponse from = createWallet(5304L, "55.00");
        WalletResponse to = createWallet(5305L, "10.00");

        performTransfer("bal-fail-1", from.getWalletId(), to.getWalletId(), "999.00")
                .andExpect(status().isBadRequest());

        assertThat(walletBalance(from.getWalletId())).isEqualByComparingTo("55.00");
        assertThat(walletBalance(to.getWalletId())).isEqualByComparingTo("10.00");

        StatementResponse fromStmt = getStatementByWalletId(from.getWalletId());
        StatementResponse toStmt = getStatementByWalletId(to.getWalletId());
        assertThat(fromStmt.getCurrentBalance()).isEqualByComparingTo("55.00");
        assertThat(toStmt.getCurrentBalance()).isEqualByComparingTo("10.00");
        // Only opening funding credits — no transfer ledger rows after failed attempt.
        assertThat(fromStmt.getEntries()).hasSize(1);
        assertThat(fromStmt.getEntries().get(0).getTransferId()).isNull();
        assertThat(toStmt.getEntries()).hasSize(1);
        assertThat(toStmt.getEntries().get(0).getTransferId()).isNull();
    }
}
