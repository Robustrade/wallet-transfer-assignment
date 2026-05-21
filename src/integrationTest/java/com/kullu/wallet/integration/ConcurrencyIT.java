package com.kullu.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.springframework.beans.factory.annotation.Autowired;

import com.kullu.wallet.dto.request.CreateTransferRequest;
import com.kullu.wallet.dto.response.TransferOutcome;
import com.kullu.wallet.entity.LedgerEntry;
import com.kullu.wallet.exception.IdempotencyConflictException;
import com.kullu.wallet.repository.IdempotencyRecordRepository;
import com.kullu.wallet.repository.LedgerEntryRepository;
import com.kullu.wallet.repository.TransferRepository;
import com.kullu.wallet.repository.WalletRepository;
import com.kullu.wallet.service.TransferService;
import com.kullu.wallet.support.AbstractPostgresIntegrationTest;

class ConcurrencyIT extends AbstractPostgresIntegrationTest {

    @Autowired
    private TransferService transferService;
    @Autowired
    private WalletRepository walletRepo;
    @Autowired
    private TransferRepository transferRepo;
    @Autowired
    private LedgerEntryRepository ledgerRepo;
    @Autowired
    private IdempotencyRecordRepository idempotencyRepo;

    @BeforeEach
    void clean() {
        ledgerRepo.deleteAllInBatch();
        idempotencyRepo.deleteAllInBatch();
        transferRepo.deleteAllInBatch();
        walletRepo.deleteAllInBatch();
    }

    /**
     * C1: 50 threads attempt to debit the same wallet funded for exactly 30
     * transfers. Exactly 30 must succeed, 20 must fail with INSUFFICIENT_FUNDS,
     * final balance is zero, ledger has 60 rows and balances to zero.
     */
    @Test
    @Timeout(value = 60, unit = TimeUnit.SECONDS)
    void c1DoubleSpendIsImpossible() throws Exception {
        long amount = 100;
        int succeed = 30;
        int total = 50;
        String from = seedWallet(walletRepo, amount * succeed);
        String to = seedWallet(walletRepo, 0);

        AtomicInteger processed = new AtomicInteger();
        AtomicInteger failed = new AtomicInteger();
        runInParallel(total, i -> {
            CreateTransferRequest req = new CreateTransferRequest(
                "c1-" + i, from, to, amount);
            TransferOutcome out = transferService.createTransfer(req);
            if (out.getStatus() == 201) {
                processed.incrementAndGet();
            } else if (out.getStatus() == 422) {
                failed.incrementAndGet();
            }
        });

        assertThat(processed.get()).isEqualTo(succeed);
        assertThat(failed.get()).isEqualTo(total - succeed);
        assertThat(walletRepo.findById(from).orElseThrow().getBalance()).isZero();
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(amount * succeed);
        assertThat(ledgerRepo.count()).isEqualTo(succeed * 2L);
        assertLedgerZeroSum();
        assertBalanceEqualsLedger(from);
        assertBalanceEqualsLedger(to);
    }

    /**
     * C2: 100 threads submit the SAME idempotency key in parallel. Exactly
     * one transfer must be created and all 100 responses must be byte-identical.
     */
    @Test
    @Timeout(value = 60, unit = TimeUnit.SECONDS)
    void c2ConcurrentDuplicatesCollapseToOneTransfer() throws Exception {
        long amount = 50;
        String from = seedWallet(walletRepo, 10_000);
        String to = seedWallet(walletRepo, 0);

        int total = 100;
        String key = "c2-shared-key";
        List<String> bodies = new java.util.concurrent.CopyOnWriteArrayList<>();
        AtomicInteger inFlightConflicts = new AtomicInteger();

        runInParallel(total, i -> {
            try {
                TransferOutcome out = transferService.createTransfer(
                    new CreateTransferRequest(key, from, to, amount));
                bodies.add(out.getResponseBody());
            } catch (IdempotencyConflictException e) {
                // A concurrent duplicate that lost the unique-key race before
                // the original committed. The contract is that the caller
                // retries; the window between tryClaim and storeResponse means
                // a single retry can still hit IN_FLIGHT, so we retry in a
                // bounded loop with backoff.
                inFlightConflicts.incrementAndGet();
                TransferOutcome retry = null;
                for (int attempt = 0; attempt < 10; attempt++) {
                    try {
                        retry = transferService.createTransfer(
                            new CreateTransferRequest(key, from, to, amount));
                        break;
                    } catch (IdempotencyConflictException ex) {
                        if (attempt == 9) throw e;
                        Thread.sleep(10);
                    }
                }
                bodies.add(retry.getResponseBody());
            }
        });

        assertThat(bodies).hasSize(total);
        // All bodies identical → exactly-once API semantics.
        assertThat(bodies.stream().distinct().toList()).hasSize(1);

        // Exactly one transfer + exactly two ledger rows persisted.
        assertThat(transferRepo.count()).isEqualTo(1);
        assertThat(ledgerRepo.count()).isEqualTo(2);
        assertThat(walletRepo.findById(from).orElseThrow().getBalance()).isEqualTo(10_000 - amount);
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(amount);
    }

    /**
     * C3: symmetric reverse-direction transfers (A→B and B→A) must complete
     * without deadlock thanks to ordered locking on wallet IDs.
     */
    @Test
    @Timeout(value = 60, unit = TimeUnit.SECONDS)
    void c3ReverseDirectionTransfersAreDeadlockFree() throws Exception {
        long amount = 10;
        String a = seedWallet(walletRepo, 10_000);
        String b = seedWallet(walletRepo, 10_000);

        int perDirection = 50;
        int total = perDirection * 2;
        AtomicInteger ok = new AtomicInteger();

        runInParallel(total, i -> {
            boolean aToB = i < perDirection;
            String src = aToB ? a : b;
            String dst = aToB ? b : a;
            TransferOutcome out = transferService.createTransfer(
                new CreateTransferRequest("c3-" + i, src, dst, amount));
            if (out.getStatus() == 201) ok.incrementAndGet();
        });

        // No deadlock errors surface; everything completes successfully.
        assertThat(ok.get()).isEqualTo(total);
        // Net algebraic effect: identical traffic both directions ⇒ unchanged.
        assertThat(walletRepo.findById(a).orElseThrow().getBalance()).isEqualTo(10_000);
        assertThat(walletRepo.findById(b).orElseThrow().getBalance()).isEqualTo(10_000);

        assertLedgerZeroSum();
        assertBalanceEqualsLedger(a);
        assertBalanceEqualsLedger(b);
    }

    /**
     * C4: mixed workload — 40 threads sharing one idempotency key + 40 threads
     * with unique keys. Asserts collapse-to-one for the shared key, distinct
     * transfers for the unique keys, and the ledger zero-sum + balance equation.
     */
    @Test
    @Timeout(value = 90, unit = TimeUnit.SECONDS)
    void c4MixedWorkloadInvariants() throws Exception {
        long amount = 1;
        String from = seedWallet(walletRepo, 1_000);
        String to = seedWallet(walletRepo, 0);

        int shared = 40;
        int unique = 40;
        int total = shared + unique;
        String sharedKey = "c4-shared";

        runInParallel(total, i -> {
            String key = i < shared ? sharedKey : "c4-uniq-" + i;
            try {
                transferService.createTransfer(
                    new CreateTransferRequest(key, from, to, amount));
            } catch (IdempotencyConflictException e) {
                // Retry the contested shared key with backoff.
                for (int attempt = 0; attempt < 10; attempt++) {
                    try {
                        transferService.createTransfer(
                            new CreateTransferRequest(key, from, to, amount));
                        break;
                    } catch (IdempotencyConflictException ex) {
                        if (attempt == 9) throw e;
                        Thread.sleep(10);
                    }
                }
            }
        });

        // 1 shared transfer + 40 unique transfers = 41 transfer rows; double-
        // entry means 82 ledger rows.
        assertThat(transferRepo.count()).isEqualTo(41);
        assertThat(ledgerRepo.count()).isEqualTo(82);

        assertThat(walletRepo.findById(from).orElseThrow().getBalance())
            .isEqualTo(1_000 - 41);
        assertThat(walletRepo.findById(to).orElseThrow().getBalance()).isEqualTo(41);

        assertLedgerZeroSum();
        assertBalanceEqualsLedger(from);
        assertBalanceEqualsLedger(to);
    }

    // ── helpers ──────────────────────────────────────────────────────────────

    @FunctionalInterface
    private interface IndexedTask {
        void run(int i) throws Exception;
    }

    private void runInParallel(int n, IndexedTask task) throws Exception {
        ExecutorService pool = Executors.newFixedThreadPool(Math.min(n, 32));
        CountDownLatch start = new CountDownLatch(1);
        CountDownLatch done = new CountDownLatch(n);
        List<Throwable> errors = new java.util.concurrent.CopyOnWriteArrayList<>();
        for (int i = 0; i < n; i++) {
            final int idx = i;
            pool.submit(() -> {
                try {
                    start.await();
                    task.run(idx);
                } catch (Throwable t) {
                    errors.add(t);
                } finally {
                    done.countDown();
                }
            });
        }
        start.countDown();
        if (!done.await(90, TimeUnit.SECONDS)) {
            pool.shutdownNow();
            throw new AssertionError("parallel tasks did not complete in time");
        }
        pool.shutdown();
        pool.awaitTermination(5, TimeUnit.SECONDS);
        if (!errors.isEmpty()) {
            AssertionError ae = new AssertionError(
                "parallel tasks reported " + errors.size() + " errors; first: " + errors.get(0));
            ae.initCause(errors.get(0));
            throw ae;
        }
    }

    private void assertLedgerZeroSum() {
        List<LedgerEntry> all = ledgerRepo.findAll();
        long sum = all.stream().mapToLong(LedgerEntry::signedAmount).sum();
        assertThat(sum).as("ledger must sum to zero across all entries").isZero();
    }

    private void assertBalanceEqualsLedger(String walletId) {
        List<LedgerEntry> entries = ledgerRepo.findAllByWalletIdOrderByCreatedAtDesc(walletId);
        long net = entries.stream().mapToLong(LedgerEntry::signedAmount).sum();
        long stored = walletRepo.findById(walletId).orElseThrow().getBalance();
        long initial = initialBalanceOf(walletId);
        // The double-entry invariant: stored balance must equal the seeded
        // initial balance plus the net of every ledger entry touching this
        // wallet. A regression that updated the balance but skipped a ledger
        // write would leave `stored != initial + net` and fail here.
        assertThat(stored)
            .as("balance(%s) must equal initial(%d) + Σledger(%d)", walletId, initial, net)
            .isEqualTo(initial + net);
    }
}
