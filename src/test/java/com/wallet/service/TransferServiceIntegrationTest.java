package com.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;

import com.wallet.AbstractIntegrationTest;
import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.TransferStatus;
import com.wallet.domain.Wallet;
import com.wallet.exception.IdempotencyConflictException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.WalletRepository;

class TransferServiceIntegrationTest extends AbstractIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Test
    void transfer_movesFundsAndCreatesBalancedLedgerEntries() {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));

        TransferResult result = transferService.transfer(UUID.randomUUID().toString(), source.getId(),
                destination.getId(), new BigDecimal("40.00"));

        assertThat(result.isReplay()).isFalse();
        assertThat(result.getTransfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(walletRepository.findById(source.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("60.00");
        assertThat(walletRepository.findById(destination.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("40.00");

        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(result.getTransfer().getId());
        assertThat(entries).hasSize(2);
        assertThat(entries).extracting(LedgerEntry::getEntryType)
                .containsExactlyInAnyOrder(EntryType.DEBIT, EntryType.CREDIT);
        BigDecimal net = entries.stream()
                .map(e -> e.getEntryType() == EntryType.DEBIT ? e.getAmount().negate() : e.getAmount())
                .reduce(BigDecimal.ZERO, BigDecimal::add);
        assertThat(net).isEqualByComparingTo(BigDecimal.ZERO);
    }

    @Test
    void transfer_duplicateIdempotencyKeyWithSamePayload_replaysWithoutDoubleDebit() {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String key = UUID.randomUUID().toString();

        TransferResult first = transferService.transfer(key, source.getId(), destination.getId(),
                new BigDecimal("40.00"));
        TransferResult second = transferService.transfer(key, source.getId(), destination.getId(),
                new BigDecimal("40.00"));

        assertThat(first.isReplay()).isFalse();
        assertThat(second.isReplay()).isTrue();
        assertThat(second.getTransfer().getId()).isEqualTo(first.getTransfer().getId());
        assertThat(walletRepository.findById(source.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("60.00");
        assertThat(ledgerEntryRepository.findByTransferId(first.getTransfer().getId())).hasSize(2);
    }

    @Test
    void transfer_duplicateIdempotencyKeyWithDifferentPayload_throwsConflict() {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String key = UUID.randomUUID().toString();

        transferService.transfer(key, source.getId(), destination.getId(), new BigDecimal("40.00"));

        assertThatThrownBy(
                () -> transferService.transfer(key, source.getId(), destination.getId(), new BigDecimal("50.00")))
                .isInstanceOf(IdempotencyConflictException.class);
    }

    @Test
    void transfer_insufficientFunds_marksFailedWithoutLedgerEntriesOrBalanceChange() {
        Wallet source = walletRepository.insert(new BigDecimal("10.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));

        TransferResult result = transferService.transfer(UUID.randomUUID().toString(), source.getId(),
                destination.getId(), new BigDecimal("40.00"));

        assertThat(result.getTransfer().getStatus()).isEqualTo(TransferStatus.FAILED);
        assertThat(result.getTransfer().getFailureReason()).isEqualTo("insufficient funds");
        assertThat(walletRepository.findById(source.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("10.00");
        assertThat(ledgerEntryRepository.findByTransferId(result.getTransfer().getId())).isEmpty();
    }

    @Test
    void transfer_unknownWallet_throwsNotFound() {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        UUID unknownWalletId = UUID.randomUUID();

        assertThatThrownBy(() -> transferService.transfer(UUID.randomUUID().toString(), source.getId(),
                unknownWalletId, BigDecimal.TEN)).isInstanceOf(WalletNotFoundException.class);
    }

    @Test
    void concurrentTransfersFromSameWallet_neverOverdraftAndLedgerStaysBalanced() throws Exception {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        int threadCount = 20;
        BigDecimal amountPerTransfer = new BigDecimal("10.00");

        List<Future<TransferResult>> futures = runConcurrently(threadCount,
                () -> transferService.transfer(UUID.randomUUID().toString(), source.getId(), destination.getId(),
                        amountPerTransfer));

        long processed = futures.stream()
                .map(this::resultOf)
                .filter(r -> r.getTransfer().getStatus() == TransferStatus.PROCESSED)
                .count();

        Wallet finalSource = walletRepository.findById(source.getId()).orElseThrow();
        Wallet finalDestination = walletRepository.findById(destination.getId()).orElseThrow();

        assertThat(processed).isEqualTo(10);
        assertThat(finalSource.getBalance()).isEqualByComparingTo(
                new BigDecimal("100.00").subtract(amountPerTransfer.multiply(BigDecimal.valueOf(processed))));
        assertThat(finalSource.getBalance().signum()).isGreaterThanOrEqualTo(0);
        assertThat(finalDestination.getBalance())
                .isEqualByComparingTo(amountPerTransfer.multiply(BigDecimal.valueOf(processed)));

        futures.stream().map(this::resultOf).forEach(r -> {
            List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(r.getTransfer().getId());
            if (r.getTransfer().getStatus() == TransferStatus.PROCESSED) {
                assertThat(entries).hasSize(2);
                assertThat(entries).extracting(LedgerEntry::getEntryType)
                        .containsExactlyInAnyOrder(EntryType.DEBIT, EntryType.CREDIT);
                BigDecimal net = entries.stream()
                        .map(e -> e.getEntryType() == EntryType.DEBIT ? e.getAmount().negate() : e.getAmount())
                        .reduce(BigDecimal.ZERO, BigDecimal::add);
                assertThat(net).isEqualByComparingTo(BigDecimal.ZERO);
            } else {
                assertThat(entries).isEmpty();
            }
        });
    }

    @Test
    void concurrentOppositeDirectionTransfers_doNotDeadlock() throws Exception {
        Wallet walletA = walletRepository.insert(new BigDecimal("100.00"));
        Wallet walletB = walletRepository.insert(new BigDecimal("100.00"));
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService executor = Executors.newFixedThreadPool(2);

        Future<TransferResult> aToB = executor.submit(() -> {
            start.await();
            return transferService.transfer(UUID.randomUUID().toString(), walletA.getId(), walletB.getId(),
                    BigDecimal.TEN);
        });
        Future<TransferResult> bToA = executor.submit(() -> {
            start.await();
            return transferService.transfer(UUID.randomUUID().toString(), walletB.getId(), walletA.getId(),
                    BigDecimal.TEN);
        });
        start.countDown();

        TransferResult resultAtoB = aToB.get(10, TimeUnit.SECONDS);
        TransferResult resultBtoA = bToA.get(10, TimeUnit.SECONDS);
        executor.shutdown();

        assertThat(resultAtoB.getTransfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(resultBtoA.getTransfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(walletRepository.findById(walletA.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("100.00");
        assertThat(walletRepository.findById(walletB.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("100.00");
    }

    @Test
    void concurrentDuplicateIdempotencyKey_resultsInExactlyOneProcessedTransfer() throws Exception {
        Wallet source = walletRepository.insert(new BigDecimal("100.00"));
        Wallet destination = walletRepository.insert(new BigDecimal("0.00"));
        String sharedKey = UUID.randomUUID().toString();
        int threadCount = 10;

        List<Future<TransferResult>> futures = runConcurrently(threadCount,
                () -> transferService.transfer(sharedKey, source.getId(), destination.getId(),
                        new BigDecimal("10.00")));

        Set<UUID> transferIds = new HashSet<>();
        for (Future<TransferResult> future : futures) {
            transferIds.add(resultOf(future).getTransfer().getId());
        }

        assertThat(transferIds).hasSize(1);
        assertThat(walletRepository.findById(source.getId()).orElseThrow().getBalance())
                .isEqualByComparingTo("90.00");
        assertThat(ledgerEntryRepository.findByTransferId(transferIds.iterator().next())).hasSize(2);
    }

    private TransferResult resultOf(Future<TransferResult> future) {
        try {
            return future.get(10, TimeUnit.SECONDS);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    private List<Future<TransferResult>> runConcurrently(int threadCount,
            java.util.concurrent.Callable<TransferResult> task) throws InterruptedException {
        ExecutorService executor = Executors.newFixedThreadPool(threadCount);
        CountDownLatch ready = new CountDownLatch(threadCount);
        CountDownLatch start = new CountDownLatch(1);
        List<Future<TransferResult>> futures = new ArrayList<>();
        for (int i = 0; i < threadCount; i++) {
            futures.add(executor.submit(() -> {
                ready.countDown();
                start.await();
                return task.call();
            }));
        }
        ready.await();
        start.countDown();
        executor.shutdown();
        return futures;
    }
}
