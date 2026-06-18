package com.videtech.wallet_transfer;

import static org.assertj.core.api.Assertions.*;

import java.math.BigDecimal;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;

import java.util.concurrent.Executors;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.testcontainers.service.connection.ServiceConnection;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

import com.videtech.wallet_transfer.domain.LedgerEntry;
import com.videtech.wallet_transfer.domain.Transfer;
import com.videtech.wallet_transfer.domain.TransferStatus;
import com.videtech.wallet_transfer.domain.Wallet;
import com.videtech.wallet_transfer.repository.LedgerEntryRepository;
import com.videtech.wallet_transfer.repository.TransferRepository;
import com.videtech.wallet_transfer.repository.WalletRepository;
import com.videtech.wallet_transfer.service.TransferService;

@SpringBootTest
@Testcontainers
public class TransferServiceTest {

	
	@Container
    @ServiceConnection
    static PostgreSQLContainer<?> postgres = new PostgreSQLContainer<>("postgres:15");

	static {
	    java.util.TimeZone.setDefault(java.util.TimeZone.getTimeZone("UTC"));
	}
    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;
    
    @BeforeEach
    void setup() {
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();

        // fresh wallets for each test
        
        Wallet w1 = new Wallet();
        w1.setId("wallet_1");
        w1.setBalance(new BigDecimal("1000.0000"));
        w1.setCreatedAt(java.time.LocalDateTime.now());
        walletRepository.save(w1);

        Wallet w2 = new Wallet();
        w2.setId("wallet_2");
        w2.setBalance(new BigDecimal("500.0000"));
        w2.setCreatedAt(java.time.LocalDateTime.now());
        walletRepository.save(w2);
    }
    @Test
    void transfer_failedIdempotency_sameKeyReturnsSameFailure() {
        // first attempt fails
        assertThatThrownBy(() ->
                transferService.execute("key-fail-idem", "wallet_1", "wallet_2", new BigDecimal("9999"))
        ).isInstanceOf(RuntimeException.class);

        // second attempt with same key returns same failed transfer
        assertThatThrownBy(() ->
                transferService.execute("key-fail-idem", "wallet_1", "wallet_2", new BigDecimal("9999"))
        ).isInstanceOf(RuntimeException.class)
                .hasMessageContaining("Insufficient balance");

        Transfer existing = transferRepository.findByIdempotencyKey("key-fail-idem").get();
        assertThat(existing.getStatus()).isEqualTo(TransferStatus.FAILED);

        // no ledger entries created
        assertThat(ledgerEntryRepository.findByTransferId(existing.getId())).isEmpty();

        // balances unchanged
        assertThat(walletRepository.findById("wallet_1").get().getBalance())
                .isEqualByComparingTo("1000.0000");
    }
    
    @Test
    void transfer_happyPath_balancesUpdatedAndLedgerCreated() {
        Transfer result = transferService.execute("key-001", "wallet_1", "wallet_2", new BigDecimal("100"));

        assertThat(result.getStatus()).isEqualTo(TransferStatus.PROCESSED);
        assertThat(walletRepository.findById("wallet_1").get().getBalance())
                .isEqualByComparingTo("900.0000");
        assertThat(walletRepository.findById("wallet_2").get().getBalance())
                .isEqualByComparingTo("600.0000");

        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(result.getId());
        assertThat(entries).hasSize(2);
        assertThat(entries).anyMatch(e -> e.getType().name().equals("DEBIT"));
        assertThat(entries).anyMatch(e -> e.getType().name().equals("CREDIT"));
    }
    
    
    @Test
    void transfer_idempotency_duplicateKeyReturnsSameTransfer() {
        Transfer first = transferService.execute("key-002", "wallet_1", "wallet_2", new BigDecimal("100"));
        Transfer second = transferService.execute("key-002", "wallet_1", "wallet_2", new BigDecimal("100"));

        assertThat(first.getId()).isEqualTo(second.getId());
        assertThat(walletRepository.findById("wallet_1").get().getBalance())
                .isEqualByComparingTo("900.0000");
    }
    
    @Test
    void transfer_insufficientBalance_markedFailed() {
        assertThatThrownBy(() ->
                transferService.execute("key-003", "wallet_1", "wallet_2", new BigDecimal("9999"))
        ).isInstanceOf(RuntimeException.class)
                .hasMessageContaining("Insufficient balance");

        Transfer transfer = transferRepository.findByIdempotencyKey("key-003").get();
        assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
    }
    
    @Test
    void transfer_ledgerAlwaysBalanced() {
        transferService.execute("key-004", "wallet_1", "wallet_2", new BigDecimal("200"));

        List<LedgerEntry> entries = ledgerEntryRepository.findAll();
        BigDecimal debits = entries.stream()
                .filter(e -> e.getType().name().equals("DEBIT"))
                .map(LedgerEntry::getAmount)
                .reduce(BigDecimal.ZERO, BigDecimal::add);

        BigDecimal credits = entries.stream()
                .filter(e -> e.getType().name().equals("CREDIT"))
                .map(LedgerEntry::getAmount)
                .reduce(BigDecimal.ZERO, BigDecimal::add);

        assertThat(debits).isEqualByComparingTo(credits);
    }
    @Test
    void transfer_concurrency_noDoubleSpend() throws InterruptedException {
    int threads = 10;
    BigDecimal amount = new BigDecimal("150");
    ExecutorService executor = Executors.newFixedThreadPool(threads);
    CountDownLatch latch = new CountDownLatch(threads);
    java.util.concurrent.atomic.AtomicInteger successCount = new java.util.concurrent.atomic.AtomicInteger();

    for (int i = 0; i < threads; i++) {
        String key = "key-concurrent-" + i;
        executor.submit(() -> {
            try {
                transferService.execute(key, "wallet_1", "wallet_2", amount);
                successCount.incrementAndGet();
            } catch (Exception ignored) {
            } finally {
                latch.countDown();
            }
        });
    }

    latch.await();
    executor.shutdown();

    BigDecimal w1 = walletRepository.findById("wallet_1").orElseThrow().getBalance();
    BigDecimal w2 = walletRepository.findById("wallet_2").orElseThrow().getBalance();
    BigDecimal expectedW1 = new BigDecimal("1000.0000")
            .subtract(amount.multiply(BigDecimal.valueOf(successCount.get())));
    BigDecimal expectedW2 = new BigDecimal("500.0000")
            .add(amount.multiply(BigDecimal.valueOf(successCount.get())));

    assertThat(w1).isEqualByComparingTo(expectedW1);
    assertThat(w2).isEqualByComparingTo(expectedW2);
    assertThat(w1).isGreaterThanOrEqualTo(BigDecimal.ZERO);
    assertThat(ledgerEntryRepository.findAll()).hasSize(successCount.get() * 2);
}
}


