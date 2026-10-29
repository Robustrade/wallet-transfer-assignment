package com.example.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.example.wallet.PostgresIntegrationTest;
import com.example.wallet.domain.EntryType;
import com.example.wallet.domain.IdempotencyKeyReusedException;
import com.example.wallet.domain.InvalidTransferException;
import com.example.wallet.domain.LedgerEntry;
import com.example.wallet.domain.TransferStatus;
import com.example.wallet.domain.WalletNotFoundException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;

class TransferServiceTest extends PostgresIntegrationTest {

    @Autowired TransferService transfers;
    @Autowired WalletService wallets;
    @Autowired JdbcTemplate jdbc;

    private String wallet(long balance) {
        String id = uniqueId("w");
        wallets.createWallet(id, balance);
        return id;
    }

    private long balance(String walletId) {
        return wallets.getWallet(walletId).balance();
    }

    private TransferResult transfer(String key, String from, String to, long amount) {
        return transfers.createTransfer(new CreateTransferCommand(key, from, to, amount));
    }

    private int transferRowsWithKey(String key) {
        return jdbc.queryForObject("SELECT count(*) FROM transfers WHERE idempotency_key = ?", Integer.class, key);
    }

    // ---------- transfer execution + ledger ----------

    @Test
    void processedTransferMovesMoneyAndWritesBalancedLedgerEntries() {
        String a = wallet(500);
        String b = wallet(50);

        TransferResult result = transfer(uniqueId("k"), a, b, 100);

        assertThat(result.replayed()).isFalse();
        assertThat(result.transfer().status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(balance(a)).isEqualTo(400);
        assertThat(balance(b)).isEqualTo(150);

        List<LedgerEntry> entries = transfers.getLedgerEntries(result.transfer().id());
        assertThat(entries).hasSize(2);
        assertThat(entries).filteredOn(e -> e.type() == EntryType.DEBIT)
                .singleElement().satisfies(e -> {
                    assertThat(e.walletId()).isEqualTo(a);
                    assertThat(e.amount()).isEqualTo(100);
                });
        assertThat(entries).filteredOn(e -> e.type() == EntryType.CREDIT)
                .singleElement().satisfies(e -> {
                    assertThat(e.walletId()).isEqualTo(b);
                    assertThat(e.amount()).isEqualTo(100);
                });
    }

    @Test
    void transferOfEntireBalanceIsAllowed() {
        String a = wallet(100);
        String b = wallet(0);

        assertThat(transfer(uniqueId("k"), a, b, 100).transfer().status()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(balance(a)).isZero();
        assertThat(balance(b)).isEqualTo(100);
    }

    // ---------- failure scenarios ----------

    @Test
    void insufficientFundsMarksTransferFailedWithoutMovingMoneyOrWritingLedger() {
        String a = wallet(50);
        String b = wallet(0);

        TransferResult result = transfer(uniqueId("k"), a, b, 100);

        assertThat(result.transfer().status()).isEqualTo(TransferStatus.FAILED);
        assertThat(result.transfer().failureReason()).isEqualTo("INSUFFICIENT_FUNDS");
        assertThat(balance(a)).isEqualTo(50);
        assertThat(balance(b)).isZero();
        assertThat(transfers.getLedgerEntries(result.transfer().id())).isEmpty();
    }

    @Test
    void failedTransferIsReplayedAsFailedEvenIfFundsArriveLater() {
        String a = wallet(50);
        String b = wallet(0);
        String funder = wallet(1000);
        String key = uniqueId("k");

        TransferResult first = transfer(key, a, b, 100);
        transfer(uniqueId("fund"), funder, a, 500); // now A could afford it
        TransferResult retry = transfer(key, a, b, 100);

        assertThat(first.transfer().status()).isEqualTo(TransferStatus.FAILED);
        assertThat(retry.replayed()).isTrue();
        assertThat(retry.transfer().id()).isEqualTo(first.transfer().id());
        assertThat(retry.transfer().status()).isEqualTo(TransferStatus.FAILED);
        assertThat(balance(a)).isEqualTo(550); // not debited by the retry
    }

    @Test
    void unknownWalletIsRejectedAndLeavesNoTraceSoTheKeyCanBeReused() {
        String a = wallet(100);
        String key = uniqueId("k");

        assertThatThrownBy(() -> transfer(key, a, "does-not-exist", 10))
                .isInstanceOf(WalletNotFoundException.class);

        assertThat(transferRowsWithKey(key)).isZero(); // whole transaction rolled back
        assertThat(balance(a)).isEqualTo(100);

        String b = wallet(0);
        assertThat(transfer(key, a, b, 10).transfer().status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void invalidRequestsAreRejectedBeforeAnythingIsWritten() {
        String a = wallet(100);
        String key = uniqueId("k");

        assertThatThrownBy(() -> transfer(key, a, a, 10)).isInstanceOf(InvalidTransferException.class);
        assertThatThrownBy(() -> transfer(key, a, wallet(0), 0)).isInstanceOf(InvalidTransferException.class);
        assertThatThrownBy(() -> transfer(key, a, wallet(0), -1)).isInstanceOf(InvalidTransferException.class);
        assertThat(transferRowsWithKey(key)).isZero();
    }

    // ---------- idempotency ----------

    @Test
    void duplicateRequestReturnsOriginalResultAndDoesNotTransferTwice() {
        String a = wallet(500);
        String b = wallet(0);
        String key = uniqueId("k");

        TransferResult first = transfer(key, a, b, 100);
        TransferResult second = transfer(key, a, b, 100);
        TransferResult third = transfer(key, a, b, 100);

        assertThat(first.replayed()).isFalse();
        assertThat(second.replayed()).isTrue();
        assertThat(third.replayed()).isTrue();
        assertThat(second.transfer().id()).isEqualTo(first.transfer().id());
        assertThat(third.transfer().id()).isEqualTo(first.transfer().id());
        assertThat(balance(a)).isEqualTo(400);
        assertThat(balance(b)).isEqualTo(100);
        assertThat(transfers.getLedgerEntries(first.transfer().id())).hasSize(2);
    }

    @Test
    void reusingAKeyWithADifferentPayloadIsRejected() {
        String a = wallet(500);
        String b = wallet(0);
        String key = uniqueId("k");

        transfer(key, a, b, 100);

        assertThatThrownBy(() -> transfer(key, a, b, 999)).isInstanceOf(IdempotencyKeyReusedException.class);
        assertThatThrownBy(() -> transfer(key, a, wallet(0), 100))
                .isInstanceOf(IdempotencyKeyReusedException.class);
        assertThat(balance(a)).isEqualTo(400);
    }

    // ---------- concurrency ----------

    @Test
    void concurrentDuplicatesOfTheSameKeyExecuteExactlyOnce() throws Exception {
        String a = wallet(1000);
        String b = wallet(0);
        String key = uniqueId("k");
        int threads = 12;

        List<TransferResult> results = runConcurrently(threads, () -> transfer(key, a, b, 100));

        assertThat(results).hasSize(threads);
        assertThat(results.stream().map(r -> r.transfer().id()).distinct()).hasSize(1);
        assertThat(results.stream().filter(r -> !r.replayed())).hasSize(1);
        assertThat(balance(a)).isEqualTo(900);
        assertThat(balance(b)).isEqualTo(100);
        assertThat(transferRowsWithKey(key)).isEqualTo(1);
        assertThat(transfers.getLedgerEntries(results.get(0).transfer().id())).hasSize(2);
    }

    @Test
    void concurrentDebitsOnTheSameWalletNeverOverspend() throws Exception {
        String source = wallet(100);
        String sink = wallet(0);
        int attempts = 20; // 20 x 10 = 200 requested, only 100 available

        List<TransferResult> results = runConcurrently(attempts, () -> transfer(uniqueId("k"), source, sink, 10));

        long processed = results.stream().filter(r -> r.transfer().status() == TransferStatus.PROCESSED).count();
        long failed = results.stream().filter(r -> r.transfer().status() == TransferStatus.FAILED).count();
        assertThat(processed).isEqualTo(10);
        assertThat(failed).isEqualTo(10);
        assertThat(balance(source)).isZero();
        assertThat(balance(sink)).isEqualTo(100);
        assertLedgerMatchesBalance(source, 100);
        assertLedgerMatchesBalance(sink, 0);
    }

    @Test
    void oppositeDirectionTransfersDoNotDeadlockAndConserveMoney() throws Exception {
        String a = wallet(1000);
        String b = wallet(1000);
        int perDirection = 15;

        List<Callable<TransferResult>> tasks = new ArrayList<>();
        for (int i = 0; i < perDirection; i++) {
            tasks.add(() -> transfer(uniqueId("ab"), a, b, 10));
            tasks.add(() -> transfer(uniqueId("ba"), b, a, 10));
        }
        List<TransferResult> results = runAll(tasks);

        assertThat(results).allSatisfy(r -> assertThat(r.transfer().status()).isEqualTo(TransferStatus.PROCESSED));
        assertThat(balance(a) + balance(b)).isEqualTo(2000);
        assertThat(balance(a)).isEqualTo(1000); // equal flow each way
        assertLedgerMatchesBalance(a, 1000);
        assertLedgerMatchesBalance(b, 1000);
    }

    // ---------- ledger helpers ----------

    /** balance must equal opening balance + credits - debits, i.e. the ledger explains every change. */
    private void assertLedgerMatchesBalance(String walletId, long openingBalance) {
        long credits = sum(walletId, EntryType.CREDIT);
        long debits = sum(walletId, EntryType.DEBIT);
        assertThat(openingBalance + credits - debits).isEqualTo(balance(walletId));
    }

    private long sum(String walletId, EntryType type) {
        return jdbc.queryForObject(
                "SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE wallet_id = ? AND entry_type = ?",
                Long.class, walletId, type.name());
    }

    // ---------- concurrency helpers ----------

    private List<TransferResult> runConcurrently(int threads, Callable<TransferResult> task) throws Exception {
        List<Callable<TransferResult>> tasks = new ArrayList<>();
        for (int i = 0; i < threads; i++) {
            tasks.add(task);
        }
        return runAll(tasks);
    }

    /** Starts all tasks at the same instant (start gate) to maximise contention. */
    private List<TransferResult> runAll(List<Callable<TransferResult>> tasks) throws Exception {
        ExecutorService pool = Executors.newFixedThreadPool(tasks.size());
        CountDownLatch ready = new CountDownLatch(tasks.size());
        CountDownLatch go = new CountDownLatch(1);
        try {
            List<Future<TransferResult>> futures = new ArrayList<>();
            for (Callable<TransferResult> task : tasks) {
                futures.add(pool.submit(() -> {
                    ready.countDown();
                    go.await();
                    return task.call();
                }));
            }
            assertThat(ready.await(10, TimeUnit.SECONDS)).isTrue();
            go.countDown();
            List<TransferResult> results = new ArrayList<>();
            for (Future<TransferResult> f : futures) {
                results.add(f.get(30, TimeUnit.SECONDS));
            }
            return results;
        } finally {
            pool.shutdownNow();
        }
    }
}
