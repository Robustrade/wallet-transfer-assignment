package com.wallet.concurrency;

import com.wallet.domain.Wallet;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.service.TransferService;
import com.wallet.dto.CreateTransferRequest;
import com.wallet.dto.TransferResponse;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;

import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import java.util.stream.Collectors;
import java.util.stream.IntStream;

import static org.assertj.core.api.Assertions.assertThat;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class ConcurrentTransferTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    private String newWallet(BigDecimal balance) {
        String id = "w_" + UUID.randomUUID();
        walletRepository.save(new Wallet(id, balance));
        return id;
    }

    @Test
    void concurrentTransfersFromSameWalletNeverOverdraw() throws Exception {
        String from = newWallet(new BigDecimal("100.00"));
        String to = newWallet(new BigDecimal("0.00"));

        int attempts = 20;
        BigDecimal amountEach = new BigDecimal("10.00"); // 20 * 10 = 200, only 10 attempts can succeed against a 100 balance

        List<TransferResponse> results = runConcurrently(attempts, i ->
                transferService.createTransfer(
                        new CreateTransferRequest(UUID.randomUUID().toString(), from, to, amountEach)));

        long processed = results.stream().filter(r -> r.status().equals("PROCESSED")).count();
        long failed = results.stream().filter(r -> r.status().equals("FAILED")).count();

        assertThat(processed).isEqualTo(10);
        assertThat(failed).isEqualTo(10);

        Wallet finalFrom = walletRepository.findById(from).orElseThrow();
        Wallet finalTo = walletRepository.findById(to).orElseThrow();

        // The invariant that actually matters: balance never went negative,
        // and the ledger agrees with the wallet balance exactly.
        assertThat(finalFrom.getBalance()).isEqualByComparingTo(BigDecimal.ZERO);
        assertThat(finalTo.getBalance()).isEqualByComparingTo(new BigDecimal("100.00"));
    }

    @Test
    void concurrentRequestsWithSameIdempotencyKeyExecuteExactlyOnce() throws Exception {
        String from = newWallet(new BigDecimal("1000.00"));
        String to = newWallet(new BigDecimal("0.00"));
        String sharedKey = UUID.randomUUID().toString();
        BigDecimal amount = new BigDecimal("50.00");

        int attempts = 15;
        List<TransferResponse> results = runConcurrently(attempts, i ->
                transferService.createTransfer(new CreateTransferRequest(sharedKey, from, to, amount)));

        long distinctTransferIds = results.stream().map(TransferResponse::transferId).distinct().count();
        assertThat(distinctTransferIds).isEqualTo(1);

        long replayedCount = results.stream().filter(TransferResponse::replayed).count();
        assertThat(replayedCount).isEqualTo(attempts - 1);

        // Money moved exactly once, no matter how many duplicate requests raced.
        assertThat(walletRepository.findById(from).orElseThrow().getBalance()).isEqualByComparingTo("950.00");
        assertThat(walletRepository.findById(to).orElseThrow().getBalance()).isEqualByComparingTo("50.00");

        UUID transferId = results.get(0).transferId();
        assertThat(ledgerEntryRepository.findByTransferId(transferId)).hasSize(2);
    }

    private <T> List<T> runConcurrently(int count, IndexedTask<T> task) throws InterruptedException {
        ExecutorService pool = Executors.newFixedThreadPool(Math.min(count, 20));
        CountDownLatch startGate = new CountDownLatch(1);
        List<Callable<T>> callables = IntStream.range(0, count)
                .<Callable<T>>mapToObj(i -> () -> {
                    startGate.await();
                    return task.run(i);
                })
                .collect(Collectors.toList());
        try {
            List<Future<T>> futures = new java.util.ArrayList<>();
            for (Callable<T> c : callables) {
                futures.add(pool.submit(c));
            }
            startGate.countDown(); // release all threads at once to maximize actual contention
            List<T> results = new java.util.ArrayList<>();
            for (Future<T> f : futures) {
                try {
                    results.add(f.get(30, TimeUnit.SECONDS));
                } catch (Exception e) {
                    throw new RuntimeException("Concurrent task failed", e);
                }
            }
            return results;
        } finally {
            pool.shutdownNow();
        }
    }

    @FunctionalInterface
    private interface IndexedTask<T> {
        T run(int index) throws Exception;
    }
}
