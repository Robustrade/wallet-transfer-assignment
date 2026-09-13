package com.wallettransfer.service;

import com.wallettransfer.api.TransferRequest;
import com.wallettransfer.api.TransferResponse;
import com.wallettransfer.api.WalletResponse;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.TestInstance;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;

@SpringBootTest
@Testcontainers
@TestInstance(TestInstance.Lifecycle.PER_CLASS)
class TransferServicePostgresTest {
    @Container
    static final PostgreSQLContainer<?> POSTGRES = new PostgreSQLContainer<>("postgres:16-alpine");

    @Autowired
    private TransferService service;

    @Autowired
    private JdbcTemplate jdbc;

    @DynamicPropertySource
    static void databaseProperties(DynamicPropertyRegistry registry) {
        registry.add("spring.datasource.url", POSTGRES::getJdbcUrl);
        registry.add("spring.datasource.username", POSTGRES::getUsername);
        registry.add("spring.datasource.password", POSTGRES::getPassword);
    }

    @BeforeAll
    void cleanDatabase() {
        jdbc.update("TRUNCATE TABLE ledger_entries, transfers, wallets CASCADE");
    }

    @AfterAll
    void stopContainer() {
        POSTGRES.stop();
    }

    @Test
    void persistsExactlyOneDebitAndCreditForProcessedTransfer() {
        WalletResponse source = service.createWallet(new BigDecimal("100.00"));
        WalletResponse destination = service.createWallet(BigDecimal.ZERO);

        TransferResponse transfer = service.create(new TransferRequest("postgres-ledger-1", source.walletId(),
                destination.walletId(), new BigDecimal("25.00")));

        assertEquals("PROCESSED", transfer.status().name());
        List<String> entryTypes = jdbc.queryForList(
                "SELECT entry_type FROM ledger_entries WHERE transfer_id = ? ORDER BY entry_type",
                String.class, transfer.transferId());
        assertEquals(List.of("CREDIT", "DEBIT"), entryTypes);
        BigDecimal ledgerBalance = jdbc.queryForObject(
                "SELECT COALESCE(SUM(CASE WHEN entry_type = 'CREDIT' THEN amount ELSE -amount END), 0) "
                        + "FROM ledger_entries WHERE transfer_id = ?", BigDecimal.class, transfer.transferId());
        assertEquals(BigDecimal.ZERO.setScale(4), ledgerBalance);
    }

    @Test
    void concurrentDebitsCannotSpendTheSameFundsTwice() throws Exception {
        WalletResponse source = service.createWallet(new BigDecimal("100.00"));
        WalletResponse destinationOne = service.createWallet(BigDecimal.ZERO);
        WalletResponse destinationTwo = service.createWallet(BigDecimal.ZERO);
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService executor = Executors.newFixedThreadPool(2);

        Future<Object> first = executor.submit(() -> transferAfter(start, "concurrent-1", source.walletId(),
                destinationOne.walletId()));
        Future<Object> second = executor.submit(() -> transferAfter(start, "concurrent-2", source.walletId(),
                destinationTwo.walletId()));
        start.countDown();

        Object firstResult = first.get();
        Object secondResult = second.get();
        executor.shutdownNow();

        assertEquals(1, List.of(firstResult, secondResult).stream()
                .filter(TransferResponse.class::isInstance).count());
        assertEquals(1, List.of(firstResult, secondResult).stream()
                .filter(InsufficientFundsException.class::isInstance).count());
        assertEquals(new BigDecimal("0.0000"), service.getWallet(source.walletId()).balance());
        assertEquals(2, jdbc.queryForObject("SELECT COUNT(*) FROM ledger_entries", Integer.class));
    }

        @Test
        void concurrentDuplicateRequestsProduceOneTransferAndOneLedgerPair() throws Exception {
        WalletResponse source = service.createWallet(new BigDecimal("100.00"));
        WalletResponse destination = service.createWallet(BigDecimal.ZERO);
        CountDownLatch start = new CountDownLatch(1);
        ExecutorService executor = Executors.newFixedThreadPool(2);
        TransferRequest request = new TransferRequest("duplicate-concurrent", source.walletId(),
            destination.walletId(), new BigDecimal("25.00"));

        Future<TransferResponse> first = executor.submit(() -> createAfter(start, request));
        Future<TransferResponse> second = executor.submit(() -> createAfter(start, request));
        start.countDown();

        TransferResponse firstResult = first.get();
        TransferResponse secondResult = second.get();
        executor.shutdownNow();

        assertEquals(firstResult.transferId(), secondResult.transferId());
        assertEquals(new BigDecimal("75.0000"), service.getWallet(source.walletId()).balance());
        assertEquals(2, jdbc.queryForObject("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?",
            Integer.class, firstResult.transferId()));
        }

        @Test
        void retryProcessesAnOrphanedPendingTransfer() {
        WalletResponse source = service.createWallet(new BigDecimal("100.00"));
        WalletResponse destination = service.createWallet(BigDecimal.ZERO);
        UUID transferId = UUID.randomUUID();
        jdbc.update("INSERT INTO transfers(id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) "
                + "VALUES (?, ?, ?, ?, ?, 'PENDING')", transferId, "orphaned-pending", source.walletId(),
            destination.walletId(), new BigDecimal("40.00"));

        TransferResponse result = service.create(new TransferRequest("orphaned-pending", source.walletId(),
            destination.walletId(), new BigDecimal("40.00")));

        assertEquals(transferId, result.transferId());
        assertEquals("PROCESSED", result.status().name());
        assertEquals(new BigDecimal("60.0000"), service.getWallet(source.walletId()).balance());
        assertEquals(2, jdbc.queryForObject("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?",
            Integer.class, transferId));
        }

    private Object transferAfter(CountDownLatch start, String key, String source, String destination) {
        try {
            start.await();
            return service.create(new TransferRequest(key, source, destination, new BigDecimal("100.00")));
        } catch (InsufficientFundsException exception) {
            return exception;
        } catch (Exception exception) {
            throw new RuntimeException(exception);
        }
    }

    private TransferResponse createAfter(CountDownLatch start, TransferRequest request) {
        try {
            start.await();
            return service.create(request);
        } catch (Exception exception) {
            throw new RuntimeException(exception);
        }
    }
}