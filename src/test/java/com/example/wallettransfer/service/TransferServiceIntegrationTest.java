package com.example.wallettransfer.service;

import com.example.wallettransfer.dto.CreateTransferRequest;
import com.example.wallettransfer.dto.TransferResponse;
import com.example.wallettransfer.domain.TransferStatus;
import com.example.wallettransfer.exception.IdempotencyConflictException;
import com.example.wallettransfer.testcontainer.PostgresTestContainer;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.dao.DataIntegrityViolationException;
import org.springframework.transaction.PlatformTransactionManager;
import org.springframework.transaction.support.TransactionTemplate;

import java.math.BigDecimal;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class TransferServiceIntegrationTest extends PostgresTestContainer {

    private static final long CONCURRENCY_TIMEOUT_SECONDS = 10;

    @Autowired
    private TransferService transferService;

    @Autowired
    private PlatformTransactionManager transactionManager;

    @Test
    void successfulTransferUpdatesBalancesAndWritesBalancedDoubleEntryLedger() {
        createWallet("source", "1000.0000");
        createWallet("destination", "200.0000");

        TransferResponse response = transferService.createTransfer(request(
                "success-key", "source", "destination", "125.50"));

        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(balance("source")).isEqualByComparingTo("874.5000");
        assertThat(balance("destination")).isEqualByComparingTo("325.5000");

        List<LedgerRow> entries = ledger(response.transferId());
        assertThat(entries).hasSize(2);
        assertThat(entries).extracting(LedgerRow::type)
                .containsExactlyInAnyOrder("DEBIT", "CREDIT");
        assertThat(entries).allSatisfy(entry ->
                assertThat(entry.amount()).isEqualByComparingTo("125.5000"));
    }

    @Test
    void replayWithSameIdempotencyKeyAndPayloadReturnsOriginalTransferWithoutSideEffects() {
        createWallet("source", "500.0000");
        createWallet("destination", "0.0000");
        CreateTransferRequest request = request("replay-key", "source", "destination", "100");

        TransferResponse first = transferService.createTransfer(request);
        TransferResponse replay = transferService.createTransfer(request);

        assertThat(replay).isEqualTo(first);
        assertThat(balance("source")).isEqualByComparingTo("400.0000");
        assertThat(balance("destination")).isEqualByComparingTo("100.0000");
        assertThat(ledger(first.transferId())).hasSize(2);
        assertThat(count("transfers")).isEqualTo(1);
        assertThat(count("idempotency_records")).isEqualTo(1);
    }

    @Test
    void sameIdempotencyKeyWithDifferentPayloadIsRejected() {
        createWallet("source", "500.0000");
        createWallet("destination", "0.0000");
        transferService.createTransfer(request("conflict-key", "source", "destination", "100"));

        assertThatThrownBy(() -> transferService.createTransfer(
                request("conflict-key", "source", "destination", "101")))
                .isInstanceOf(IdempotencyConflictException.class);

        assertThat(balance("source")).isEqualByComparingTo("400.0000");
        assertThat(count("transfers")).isEqualTo(1);
        assertThat(count("ledger_entries")).isEqualTo(2);
    }

    @Test
    void insufficientFundsPersistsFailedResultAndReplayDoesNotRetryTheDebit() {
        createWallet("source", "50.0000");
        createWallet("destination", "10.0000");
        CreateTransferRequest request = request("failed-key", "source", "destination", "75");

        TransferResponse first = transferService.createTransfer(request);
        TransferResponse replay = transferService.createTransfer(request);

        assertThat(first.status()).isEqualTo(TransferStatus.FAILED);
        assertThat(first.failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
        assertThat(replay.transferId()).isEqualTo(first.transferId());
        assertThat(replay.status()).isEqualTo(TransferStatus.FAILED);
        assertThat(balance("source")).isEqualByComparingTo("50.0000");
        assertThat(balance("destination")).isEqualByComparingTo("10.0000");
        assertThat(count("ledger_entries")).isZero();
        assertThat(count("transfers")).isEqualTo(1);
    }

    @Test
    void twoSimultaneousFullBalanceDebitsAllowOnlyOneProcessedTransfer() throws Exception {
        createWallet("source", "1000.0000");
        createWallet("destination-a", "0.0000");
        createWallet("destination-b", "0.0000");

        CountDownLatch ready = new CountDownLatch(2);
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService executor = Executors.newFixedThreadPool(2);
        try {
            Future<TransferResponse> first = executor.submit(() -> concurrentTransfer(
                    ready, start, request("concurrent-a", "source", "destination-a", "1000")));
            Future<TransferResponse> second = executor.submit(() -> concurrentTransfer(
                    ready, start, request("concurrent-b", "source", "destination-b", "1000")));

            assertThat(ready.await(CONCURRENCY_TIMEOUT_SECONDS, TimeUnit.SECONDS)).isTrue();
            start.countDown();

            List<TransferResponse> responses = List.of(
                    first.get(CONCURRENCY_TIMEOUT_SECONDS, TimeUnit.SECONDS),
                    second.get(CONCURRENCY_TIMEOUT_SECONDS, TimeUnit.SECONDS));
            assertThat(responses).extracting(TransferResponse::status)
                    .containsExactlyInAnyOrder(TransferStatus.PROCESSED, TransferStatus.FAILED);
        } finally {
            executor.shutdownNow();
        }

        assertThat(balance("source")).isEqualByComparingTo("0.0000");
        assertThat(balance("destination-a").add(balance("destination-b")))
                .isEqualByComparingTo("1000.0000");
        assertThat(count("ledger_entries")).isEqualTo(2);
    }

    @Test
    void simultaneousDuplicateRequestsExecuteOnlyOnceAndAllCallersReceiveSameTransfer() throws Exception {
        createWallet("source", "1000.0000");
        createWallet("destination", "0.0000");
        CreateTransferRequest request = request("same-key", "source", "destination", "100");

        int callers = 8;
        CountDownLatch ready = new CountDownLatch(callers);
        CountDownLatch start = new CountDownLatch(1);

        ExecutorService executor = Executors.newFixedThreadPool(callers);
        try {
            List<Future<TransferResponse>> futures = java.util.stream.IntStream.range(0, callers)
                    .mapToObj(i -> executor.submit(() -> concurrentTransfer(ready, start, request)))
                    .toList();

            assertThat(ready.await(CONCURRENCY_TIMEOUT_SECONDS, TimeUnit.SECONDS)).isTrue();
            start.countDown();

            Set<UUID> transferIds = futures.stream()
                    .map(this::getWithTimeout)
                    .map(TransferResponse::transferId)
                    .collect(java.util.stream.Collectors.toSet());

            assertThat(transferIds).hasSize(1);
        } finally {
            executor.shutdownNow();
        }

        assertThat(balance("source")).isEqualByComparingTo("900.0000");
        assertThat(balance("destination")).isEqualByComparingTo("100.0000");
        assertThat(count("transfers")).isEqualTo(1);
        assertThat(count("ledger_entries")).isEqualTo(2);
        assertThat(count("idempotency_records")).isEqualTo(1);
    }

    @Test
    void processedTransferWithoutACompleteLedgerPairIsRejectedAtCommit() {
        createWallet("source", "100.0000");
        createWallet("destination", "0.0000");

        assertLedgerInvariantViolation(() -> {
            UUID transferId = UUID.randomUUID();
            insertPendingTransfer(transferId, "source", "destination", "25.0000");
            insertLedgerEntry(transferId, "source", "DEBIT", "25.0000");
            markTransferProcessed(transferId);
        });
    }

    @Test
    void processedTransferWithAPostingForTheWrongWalletIsRejectedAtCommit() {
        createWallet("source", "100.0000");
        createWallet("destination", "0.0000");
        createWallet("unrelated", "0.0000");

        assertLedgerInvariantViolation(() -> {
            UUID transferId = UUID.randomUUID();
            insertPendingTransfer(transferId, "source", "destination", "25.0000");
            insertLedgerEntry(transferId, "source", "DEBIT", "25.0000");
            insertLedgerEntry(transferId, "unrelated", "CREDIT", "25.0000");
            markTransferProcessed(transferId);
        });
    }

    @Test
    void failedTransferWithLedgerPostingsIsRejectedAtCommit() {
        createWallet("source", "100.0000");
        createWallet("destination", "0.0000");

        assertLedgerInvariantViolation(() -> {
            UUID transferId = UUID.randomUUID();
            insertPendingTransfer(transferId, "source", "destination", "25.0000");
            insertLedgerEntry(transferId, "source", "DEBIT", "25.0000");
            insertLedgerEntry(transferId, "destination", "CREDIT", "25.0000");
            jdbcTemplate.update(
                    "UPDATE transfers SET status = 'FAILED', failure_reason = 'DECLINED', completed_at = CURRENT_TIMESTAMP WHERE id = ?",
                    transferId
            );
        });
    }

    private TransferResponse concurrentTransfer(
            CountDownLatch ready,
            CountDownLatch start,
            CreateTransferRequest request
    ) throws InterruptedException {
        ready.countDown();
        start.await();
        return transferService.createTransfer(request);
    }

    private TransferResponse getWithTimeout(Future<TransferResponse> future) {
        try {
            return future.get(CONCURRENCY_TIMEOUT_SECONDS, TimeUnit.SECONDS);
        } catch (Exception exception) {
            throw new RuntimeException(exception);
        }
    }

    private CreateTransferRequest request(
            String key,
            String from,
            String to,
            String amount
    ) {
        return new CreateTransferRequest(key, from, to, new BigDecimal(amount));
    }

    private void assertLedgerInvariantViolation(Runnable transactionBody) {
        TransactionTemplate transaction = new TransactionTemplate(transactionManager);

        assertThatThrownBy(() -> transaction.executeWithoutResult(status -> transactionBody.run()))
                .isInstanceOf(DataIntegrityViolationException.class)
                .hasMessageContaining("ledger invariant");
    }

    private void insertPendingTransfer(UUID transferId, String fromWalletId, String toWalletId, String amount) {
        jdbcTemplate.update(
                """
                        INSERT INTO transfers(id, from_wallet_id, to_wallet_id, amount, status, created_at)
                        VALUES (?, ?, ?, ?::numeric, 'PENDING', CURRENT_TIMESTAMP)
                        """,
                transferId, fromWalletId, toWalletId, amount
        );
    }

    private void insertLedgerEntry(UUID transferId, String walletId, String entryType, String amount) {
        jdbcTemplate.update(
                """
                        INSERT INTO ledger_entries(id, wallet_id, transfer_id, entry_type, amount, created_at)
                        VALUES (?, ?, ?, ?, ?::numeric, CURRENT_TIMESTAMP)
                        """,
                UUID.randomUUID(), walletId, transferId, entryType, amount
        );
    }

    private void markTransferProcessed(UUID transferId) {
        jdbcTemplate.update(
                "UPDATE transfers SET status = 'PROCESSED', completed_at = CURRENT_TIMESTAMP WHERE id = ?",
                transferId
        );
    }

    private BigDecimal balance(String walletId) {
        return jdbcTemplate.queryForObject(
                "SELECT balance FROM wallets WHERE id = ?",
                BigDecimal.class,
                walletId
        );
    }

    private long count(String table) {
        Long count = jdbcTemplate.queryForObject("SELECT COUNT(*) FROM " + table, Long.class);
        return count == null ? 0 : count;
    }

    private List<LedgerRow> ledger(UUID transferId) {
        return jdbcTemplate.query(
                "SELECT entry_type, amount FROM ledger_entries WHERE transfer_id = ?",
                (rs, rowNum) -> new LedgerRow(rs.getString("entry_type"), rs.getBigDecimal("amount")),
                transferId
        );
    }

    private record LedgerRow(String type, BigDecimal amount) {
    }
}
