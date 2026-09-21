package com.example.wallet;

import com.example.wallet.api.TransferRequest;
import com.example.wallet.domain.LedgerEntryType;
import com.example.wallet.domain.TransferStatus;
import com.example.wallet.domain.Wallet;
import com.example.wallet.exception.IdempotencyConflictException;
import com.example.wallet.exception.InvalidTransferException;
import com.example.wallet.exception.ValidationException;
import com.example.wallet.exception.WalletNotFoundException;
import com.example.wallet.repository.LedgerEntryRepository;
import com.example.wallet.repository.TransferRepository;
import com.example.wallet.repository.WalletRepository;
import com.example.wallet.service.WalletService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.PostgreSQLContainer;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

import java.math.BigDecimal;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.*;

import static org.junit.jupiter.api.Assertions.*;

@SpringBootTest
@Testcontainers
class TransferIntegrationTest {

    @Container
    static final PostgreSQLContainer<?> POSTGRES =
            new PostgreSQLContainer<>("postgres:17")
                    .withDatabaseName("walletdb")
                    .withUsername("wallet")
                    .withPassword("wallet");

    @DynamicPropertySource
    static void databaseProperties(DynamicPropertyRegistry registry) {
        registry.add("spring.datasource.url", POSTGRES::getJdbcUrl);
        registry.add("spring.datasource.username", POSTGRES::getUsername);
        registry.add("spring.datasource.password", POSTGRES::getPassword);
        registry.add("spring.jpa.hibernate.ddl-auto", () -> "create-drop");
    }

    @Autowired
    WalletService service;

    @Autowired
    WalletRepository wallets;

    @Autowired
    TransferRepository transfers;

    @Autowired
    LedgerEntryRepository ledger;

    @BeforeEach
    void resetDatabase() {
        ledger.deleteAll();
        transfers.deleteAll();
        wallets.deleteAll();

        wallets.save(new Wallet(
                "wallet_1",
                new BigDecimal("1000.00"),
                "ACTIVE"
        ));

        wallets.save(new Wallet(
                "wallet_2",
                new BigDecimal("500.00"),
                "ACTIVE"
        ));

        wallets.save(new Wallet(
                "wallet_3",
                new BigDecimal("1000.00"),
                "ACTIVE"
        ));
    }

    // ============================================================
    // BASIC TRANSFER TESTS
    // ============================================================

    @Test
    void transferMovesMoneyAndCreatesExactlyTwoLedgerEntries() {

        var response = service.transfer(
                request(
                        "transfer-1",
                        "wallet_1",
                        "wallet_2",
                        "100.00"
                )
        );

        assertEquals(
                TransferStatus.PROCESSED.name(),
                response.status()
        );

        assertEquals(
                new BigDecimal("900.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("600.00"),
                balance("wallet_2")
        );

        var entries = ledger.findAll()
                .stream()
                .filter(e ->
                        e.getTransferId()
                                .equals(response.transferId()))
                .toList();

        assertEquals(2, entries.size());

        assertEquals(
                1,
                entries.stream()
                        .filter(e ->
                                e.getWalletId().equals("wallet_1")
                                        && e.getType() == LedgerEntryType.DEBIT
                                        && e.getAmount().compareTo(
                                        new BigDecimal("100.00")
                                ) == 0)
                        .count()
        );

        assertEquals(
                1,
                entries.stream()
                        .filter(e ->
                                e.getWalletId().equals("wallet_2")
                                        && e.getType() == LedgerEntryType.CREDIT
                                        && e.getAmount().compareTo(
                                        new BigDecimal("100.00")
                                ) == 0)
                        .count()
        );
    }

    // ============================================================
    // IDEMPOTENCY TESTS
    // ============================================================

    @Test
    void retryWithSameIdempotencyKeyDoesNotMoveMoneyAgain() {

        var request = request(
                "same-key",
                "wallet_1",
                "wallet_2",
                "100.00"
        );

        var first = service.transfer(request);
        var second = service.transfer(request);

        assertEquals(
                first.transferId(),
                second.transferId()
        );

        assertEquals(
                TransferStatus.PROCESSED.name(),
                second.status()
        );

        assertEquals(
                new BigDecimal("900.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("600.00"),
                balance("wallet_2")
        );

        assertEquals(
                2,
                ledger.countByTransferId(first.transferId())
        );

        assertEquals(1, transfers.count());
    }

    @Test
    void reusingIdempotencyKeyWithDifferentAmountIsRejected() {

        service.transfer(
                request(
                        "same-key",
                        "wallet_1",
                        "wallet_2",
                        "100.00"
                )
        );

        assertThrows(
                IdempotencyConflictException.class,
                () ->
                        service.transfer(
                                request(
                                        "same-key",
                                        "wallet_1",
                                        "wallet_2",
                                        "200.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("900.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("600.00"),
                balance("wallet_2")
        );

        assertEquals(1, transfers.count());
        assertEquals(2, ledger.count());
    }

    @Test
    void reusingIdempotencyKeyWithDifferentSourceWalletIsRejected() {

        service.transfer(
                request(
                        "same-key",
                        "wallet_1",
                        "wallet_2",
                        "100.00"
                )
        );

        assertThrows(
                IdempotencyConflictException.class,
                () ->
                        service.transfer(
                                request(
                                        "same-key",
                                        "wallet_3",
                                        "wallet_2",
                                        "100.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("900.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_3")
        );

        assertEquals(1, transfers.count());
        assertEquals(2, ledger.count());
    }

    @Test
    void reusingIdempotencyKeyWithDifferentDestinationWalletIsRejected() {

        service.transfer(
                request(
                        "same-key",
                        "wallet_1",
                        "wallet_2",
                        "100.00"
                )
        );

        assertThrows(
                IdempotencyConflictException.class,
                () ->
                        service.transfer(
                                request(
                                        "same-key",
                                        "wallet_1",
                                        "wallet_3",
                                        "100.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("900.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("600.00"),
                balance("wallet_2")
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_3")
        );

        assertEquals(1, transfers.count());
        assertEquals(2, ledger.count());
    }

    // ============================================================
    // VALIDATION / BUSINESS EXCEPTION TESTS
    // ============================================================

    @Test
    void selfTransferIsRejected() {

        assertThrows(
                InvalidTransferException.class,
                () ->
                        service.transfer(
                                request(
                                        "self-transfer",
                                        "wallet_1",
                                        "wallet_1",
                                        "10.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_1")
        );

        assertEquals(0, transfers.count());
        assertEquals(0, ledger.count());
    }

    @Test
    void sourceWalletDoesNotExist() {

        assertThrows(
                WalletNotFoundException.class,
                () ->
                        service.transfer(
                                request(
                                        "missing-source",
                                        "does-not-exist",
                                        "wallet_2",
                                        "10.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("500.00"),
                balance("wallet_2")
        );

        assertEquals(0, transfers.count());
        assertEquals(0, ledger.count());
    }

    @Test
    void destinationWalletDoesNotExist() {

        assertThrows(
                WalletNotFoundException.class,
                () ->
                        service.transfer(
                                request(
                                        "missing-destination",
                                        "wallet_1",
                                        "does-not-exist",
                                        "10.00"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_1")
        );

        assertEquals(0, transfers.count());
        assertEquals(0, ledger.count());
    }

    @Test
    void amountWithMoreThanTwoDecimalPlacesIsRejected() {

        assertThrows(
                ValidationException.class,
                () ->
                        service.transfer(
                                request(
                                        "invalid-scale",
                                        "wallet_1",
                                        "wallet_2",
                                        "10.001"
                                )
                        )
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("500.00"),
                balance("wallet_2")
        );

        assertEquals(0, transfers.count());
        assertEquals(0, ledger.count());
    }

    @Test
    void insufficientFundsCreatesFailedTransferWithoutChangingBalances() {

        var response = service.transfer(
                request(
                        "insufficient-funds",
                        "wallet_1",
                        "wallet_2",
                        "1000.01"
                )
        );

        assertEquals(
                TransferStatus.FAILED.name(),
                response.status()
        );

        assertEquals(
                new BigDecimal("1000.00"),
                balance("wallet_1")
        );

        assertEquals(
                new BigDecimal("500.00"),
                balance("wallet_2")
        );

        assertEquals(0, ledger.count());

        var saved = transfers
                .findById(response.transferId())
                .orElseThrow();

        assertEquals(
                TransferStatus.FAILED,
                saved.getStatus()
        );
    }

    // ============================================================
    // CONCURRENCY - SAME SOURCE WALLET
    // ============================================================

    @Test
    void concurrentTransfersNeverOverdrawSourceWallet()
            throws Exception {

        int requests = 10;

        ExecutorService executor =
                Executors.newFixedThreadPool(requests);

        CountDownLatch ready =
                new CountDownLatch(requests);

        CountDownLatch start =
                new CountDownLatch(1);

        List<Future<String>> futures =
                new ArrayList<>();

        try {

            for (int i = 0; i < requests; i++) {

                final int n = i;

                futures.add(
                        executor.submit(() -> {

                            ready.countDown();

                            assertTrue(
                                    start.await(
                                            10,
                                            TimeUnit.SECONDS
                                    )
                            );

                            return service.transfer(
                                    request(
                                            "concurrent-" + n,
                                            "wallet_1",
                                            "wallet_2",
                                            "100.00"
                                    )
                            ).status();
                        })
                );
            }

            assertTrue(
                    ready.await(
                            10,
                            TimeUnit.SECONDS
                    )
            );

            start.countDown();

            long processed = 0;

            for (Future<String> future : futures) {

                String status =
                        future.get(
                                20,
                                TimeUnit.SECONDS
                        );

                if (TransferStatus.PROCESSED.name()
                        .equals(status)) {
                    processed++;
                }
            }

            assertEquals(
                    requests,
                    processed
            );

            assertEquals(
                    new BigDecimal("0.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("1500.00"),
                    balance("wallet_2")
            );

            assertEquals(
                    10,
                    transfers.count()
            );

            assertEquals(
                    20,
                    ledger.count()
            );

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // CONCURRENCY - OVERSUBSCRIPTION
    // ============================================================

    @Test
    void concurrentTransfersCannotSpendMoreThanAvailableBalance()
            throws Exception {

        int requests = 20;

        ExecutorService executor =
                Executors.newFixedThreadPool(requests);

        CountDownLatch ready =
                new CountDownLatch(requests);

        CountDownLatch start =
                new CountDownLatch(1);

        List<Future<String>> futures =
                new ArrayList<>();

        try {

            for (int i = 0; i < requests; i++) {

                final int n = i;

                futures.add(
                        executor.submit(() -> {

                            ready.countDown();

                            assertTrue(
                                    start.await(
                                            10,
                                            TimeUnit.SECONDS
                                    )
                            );

                            return service.transfer(
                                    request(
                                            "oversubscribe-" + n,
                                            "wallet_1",
                                            "wallet_2",
                                            "100.00"
                                    )
                            ).status();
                        })
                );
            }

            assertTrue(
                    ready.await(
                            10,
                            TimeUnit.SECONDS
                    )
            );

            start.countDown();

            long processed = 0;
            long failed = 0;

            for (Future<String> future : futures) {

                String status =
                        future.get(
                                20,
                                TimeUnit.SECONDS
                        );

                if (TransferStatus.PROCESSED.name()
                        .equals(status)) {
                    processed++;
                }

                if (TransferStatus.FAILED.name()
                        .equals(status)) {
                    failed++;
                }
            }

            assertEquals(10, processed);
            assertEquals(10, failed);

            assertEquals(
                    new BigDecimal("0.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("1500.00"),
                    balance("wallet_2")
            );

            assertEquals(
                    20,
                    transfers.count()
            );

            assertEquals(
                    20,
                    ledger.count()
            );

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // CONCURRENCY - SAME IDEMPOTENCY KEY
    // ============================================================

    @Test
    void concurrentRequestsWithSameIdempotencyKeyExecuteOnlyOnce()
            throws Exception {

        int requests = 10;

        ExecutorService executor =
                Executors.newFixedThreadPool(requests);

        CountDownLatch ready =
                new CountDownLatch(requests);

        CountDownLatch start =
                new CountDownLatch(1);

        List<Future<UUID>> futures =
                new ArrayList<>();

        try {

            for (int i = 0; i < requests; i++) {

                futures.add(
                        executor.submit(() -> {

                            ready.countDown();

                            assertTrue(
                                    start.await(
                                            10,
                                            TimeUnit.SECONDS
                                    )
                            );

                            return service.transfer(
                                    request(
                                            "same-concurrent-key",
                                            "wallet_1",
                                            "wallet_2",
                                            "100.00"
                                    )
                            ).transferId();
                        })
                );
            }

            assertTrue(
                    ready.await(
                            10,
                            TimeUnit.SECONDS
                    )
            );

            start.countDown();

            List<UUID> transferIds =
                    new ArrayList<>();

            for (Future<UUID> future : futures) {
                transferIds.add(
                        future.get(
                                20,
                                TimeUnit.SECONDS
                        )
                );
            }

            UUID firstId =
                    transferIds.get(0);

            assertTrue(
                    transferIds.stream()
                            .allMatch(firstId::equals)
            );

            assertEquals(
                    1,
                    transfers.count()
            );

            assertEquals(
                    2,
                    ledger.count()
            );

            assertEquals(
                    new BigDecimal("900.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("600.00"),
                    balance("wallet_2")
            );

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // CONCURRENCY - SAME SOURCE / DIFFERENT DESTINATION
    // ============================================================

    @Test
    void concurrentTransfersFromSameSourceToDifferentDestinations()
            throws Exception {

        wallets.save(
                new Wallet(
                        "wallet_4",
                        new BigDecimal("1000.0000"),
                        "ACTIVE"
                )
        );

        int requests = 10;

        ExecutorService executor =
                Executors.newFixedThreadPool(requests);

        CountDownLatch start =
                new CountDownLatch(1);

        List<Future<String>> futures =
                new ArrayList<>();

        try {

            for (int i = 0; i < requests; i++) {

                final int n = i;

                futures.add(
                        executor.submit(() -> {

                            assertTrue(
                                    start.await(
                                            10,
                                            TimeUnit.SECONDS
                                    )
                            );

                            String destination =
                                    n % 2 == 0
                                            ? "wallet_2"
                                            : "wallet_3";

                            return service.transfer(
                                    request(
                                            "multi-destination-" + n,
                                            "wallet_1",
                                            destination,
                                            "100.00"
                                    )
                            ).status();
                        })
                );
            }

            start.countDown();

            long processed = 0;

            for (Future<String> future : futures) {

                if (TransferStatus.PROCESSED.name()
                        .equals(
                                future.get(
                                        20,
                                        TimeUnit.SECONDS
                                )
                        )) {
                    processed++;
                }
            }

            assertEquals(10, processed);

            assertEquals(
                    new BigDecimal("0.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("1000.00"),
                    balance("wallet_2")
            );

            assertEquals(
                    new BigDecimal("1500.00"),
                    balance("wallet_3")
            );

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // CONCURRENCY - OPPOSITE DIRECTIONS
    // ============================================================

    @Test
    void concurrentOppositeTransfersCompleteWithoutDeadlock()
            throws Exception {

        ExecutorService executor =
                Executors.newFixedThreadPool(2);

        CountDownLatch start =
                new CountDownLatch(1);

        try {

            Future<String> first =
                    executor.submit(() -> {

                        assertTrue(
                                start.await(
                                        10,
                                        TimeUnit.SECONDS
                                )
                        );

                        return service.transfer(
                                request(
                                        "opposite-a",
                                        "wallet_1",
                                        "wallet_2",
                                        "100.00"
                                )
                        ).status();
                    });

            Future<String> second =
                    executor.submit(() -> {

                        assertTrue(
                                start.await(
                                        10,
                                        TimeUnit.SECONDS
                                )
                        );

                        return service.transfer(
                                request(
                                        "opposite-b",
                                        "wallet_2",
                                        "wallet_1",
                                        "50.00"
                                )
                        ).status();
                    });

            start.countDown();

            assertEquals(
                    TransferStatus.PROCESSED.name(),
                    first.get(
                            20,
                            TimeUnit.SECONDS
                    )
            );

            assertEquals(
                    TransferStatus.PROCESSED.name(),
                    second.get(
                            20,
                            TimeUnit.SECONDS
                    )
            );

            assertEquals(
                    new BigDecimal("950.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("550.00"),
                    balance("wallet_2")
            );

            assertEquals(
                    2,
                    transfers.count()
            );

            assertEquals(
                    4,
                    ledger.count()
            );

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // CONCURRENCY - MIXED DUPLICATES + UNIQUE REQUESTS
    // ============================================================

    @Test
    void concurrentMixedDuplicateAndUniqueRequests()
            throws Exception {

        int requests = 10;

        ExecutorService executor =
                Executors.newFixedThreadPool(requests);

        CountDownLatch ready =
                new CountDownLatch(requests);

        CountDownLatch start =
                new CountDownLatch(1);

        List<Future<UUID>> futures =
                new ArrayList<>();

        try {

            for (int i = 0; i < requests; i++) {

                final int n = i;

                futures.add(
                        executor.submit(() -> {

                            ready.countDown();

                            assertTrue(
                                    start.await(
                                            10,
                                            TimeUnit.SECONDS
                                    )
                            );

                            String key =
                                    n < 5
                                            ? "duplicate-key"
                                            : "unique-key-" + n;

                            return service.transfer(
                                    request(
                                            key,
                                            "wallet_1",
                                            "wallet_2",
                                            "100.00"
                                    )
                            ).transferId();
                        })
                );
            }

            assertTrue(
                    ready.await(
                            10,
                            TimeUnit.SECONDS
                    )
            );

            start.countDown();

            List<UUID> ids =
                    new ArrayList<>();

            for (Future<UUID> future : futures) {
                ids.add(
                        future.get(
                                20,
                                TimeUnit.SECONDS
                        )
                );
            }

            /*
             * 5 duplicate requests = 1 actual transfer
             * 5 unique requests = 5 actual transfers
             *
             * Total = 6 transfers.
             */
            assertEquals(
                    6,
                    transfers.count()
            );

            assertEquals(
                    12,
                    ledger.count()
            );

            assertEquals(
                    new BigDecimal("400.00"),
                    balance("wallet_1")
            );

            assertEquals(
                    new BigDecimal("1100.00"),
                    balance("wallet_2")
            );

            /*
             * The five duplicate callers must receive
             * the same transfer ID.
             */
            UUID duplicateId = ids.get(0);

            for (int i = 0; i < 5; i++) {
                assertEquals(
                        duplicateId,
                        ids.get(i)
                );
            }

        } finally {
            executor.shutdownNow();
        }
    }

    // ============================================================
    // HELPERS
    // ============================================================

    private BigDecimal balance(String walletId) {

        return wallets
                .findById(walletId)
                .orElseThrow()
                .getBalance();
    }

    private TransferRequest request(
            String key,
            String from,
            String to,
            String amount
    ) {

        return new TransferRequest(
                key,
                from,
                to,
                new BigDecimal(amount)
        );
    }
}