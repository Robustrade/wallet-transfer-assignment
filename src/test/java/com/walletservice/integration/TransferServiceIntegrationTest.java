package com.walletservice.integration;

import com.walletservice.domain.entity.*;
import com.walletservice.infrastructure.repository.*;
import com.walletservice.application.service.TransferService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.transaction.annotation.Transactional;

import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.*;

/**
 * Integration tests for the wallet transfer system.
 *
 * These tests validate the complete system behavior with:
 * - Real database transactions
 * - Concurrency safety
 * - Idempotency guarantees
 * - Ledger consistency
 * - State machine transitions
 */
@SpringBootTest
@ActiveProfiles("test")
@DisplayName("Wallet Transfer Service - Integration Tests")
class TransferServiceIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private IdempotencyRecordRepository idempotencyRecordRepository;

    private String sourceWalletId;
    private String destinationWalletId;
    private Wallet sourceWallet;
    private Wallet destinationWallet;

    @BeforeEach
    void setUp() {
        // Create test wallets
        sourceWallet = Wallet.builder()
            .balance(10000L)
            .build();
        walletRepository.save(sourceWallet);
        sourceWalletId = sourceWallet.getId();

        destinationWallet = Wallet.builder()
            .balance(5000L)
            .build();
        walletRepository.save(destinationWallet);

        destinationWalletId = destinationWallet.getId();

        // Refresh to ensure we have the latest state
        sourceWallet = walletRepository.findById(sourceWallet.getId()).orElseThrow();
        destinationWallet = walletRepository.findById(destinationWallet.getId()).orElseThrow();

        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
    }

    @Test
    @DisplayName("Should successfully transfer funds between wallets")
    void testSuccessfulTransfer() {
        // Act
        Transfer transfer = transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            1000L,
            "transfer-1"
        );

        // Assert
        assertThat(transfer).isNotNull();
        assertThat(transfer.getId()).isNotNull();
        assertThat(transfer.getStatus()).isEqualTo(Transfer.TransferStatus.PROCESSED);

        // Verify wallet balances are updated
        Wallet updatedSource = walletRepository.findById(sourceWalletId).orElseThrow();
        Wallet updatedDestination = walletRepository.findById(destinationWalletId).orElseThrow();

        assertThat(updatedSource.getBalance()).isEqualTo(10000L - 1000L);
        assertThat(updatedDestination.getBalance()).isEqualTo(5000L + 1000L);

        // Verify ledger entries (exactly 2: debit and credit)
        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transfer.getId());
        assertThat(entries).hasSize(2);

        LedgerEntry debitEntry = entries.stream()
            .filter(e -> e.getEntryType() == LedgerEntry.EntryType.DEBIT)
            .findFirst()
            .orElseThrow();
        LedgerEntry creditEntry = entries.stream()
            .filter(e -> e.getEntryType() == LedgerEntry.EntryType.CREDIT)
            .findFirst()
            .orElseThrow();

        assertThat(debitEntry.getWalletId()).isEqualTo(sourceWalletId);
        assertThat(debitEntry.getAmount()).isEqualTo(1000L);

        assertThat(creditEntry.getWalletId()).isEqualTo(destinationWalletId);
        assertThat(creditEntry.getAmount()).isEqualTo(1000L);
    }

    @Test
    @DisplayName("Should ensure idempotency - duplicate request returns same transfer")
    void testIdempotency_DuplicateRequest() {
        // Act
        Transfer transfer1 = transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            1000L,
            "idempotent-key-1"
        );

        Transfer transfer2 = transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            1000L,
            "idempotent-key-1"
        );

        // Assert
        assertThat(transfer1.getId()).isEqualTo(transfer2.getId());
        assertThat(transfer1.getStatus()).isEqualTo(transfer2.getStatus());

        // Verify only one transfer and one set of ledger entries exist
        assertThat(transferRepository.findAll()).hasSize(1);
        assertThat(ledgerEntryRepository.findAll()).hasSize(2);
    }

    @Test
    @DisplayName("Should reject idempotency key reuse with different parameters")
    void testIdempotency_ConflictingParameters() {
        // Act
        transferService.createTransfer(
            sourceWalletId,
            destinationWalletId,
            1000L,
            "conflict-key"
        );

        UUID differentDestination = UUID.randomUUID();
        Wallet wallet = Wallet.builder()
            .id(differentDestination.toString())
            .balance(5000L)
            .build();
        walletRepository.save(wallet);

        // Act & Assert - same key, different destination
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                differentDestination.toString(),
                1000L,
                "conflict-key"
            )
        ).isInstanceOf(com.walletservice.application.exception.IdempotencyConflictException.class);
    }

    @Test
    @DisplayName("Should prevent insufficient balance transfers")
    void testInsufficientBalance() {
        // Arrange
        Long transferAmount = 20000L;  // More than source wallet balance

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                destinationWalletId,
                transferAmount,
                "insufficient-1"
            )
        ).isInstanceOf(IllegalStateException.class)
            .hasMessageContaining("Insufficient balance");
    }

    @Test
    @DisplayName("Should handle concurrent transfers from same source wallet")
    void testConcurrentTransfersFromSameWallet() throws InterruptedException, ExecutionException {
        // Arrange
        UUID dest1 = UUID.randomUUID();
        UUID dest2 = UUID.randomUUID();
        UUID dest3 = UUID.randomUUID();

        walletRepository.saveAll(Arrays.asList(
            Wallet.builder().id(dest1.toString()).balance(0L).build(),
            Wallet.builder().id(dest2.toString()).balance(0L).build(),
            Wallet.builder().id(dest3.toString()).balance(0L).build()
        ));

        ExecutorService executor = Executors.newFixedThreadPool(3);
        CountDownLatch latch = new CountDownLatch(3);
        AtomicInteger successCount = new AtomicInteger(0);

        // Act - submit 3 concurrent transfers from same source wallet
        for (int i = 0; i < 3; i++) {
            final int index = i;
            executor.submit(() -> {
                try {
                    UUID destination = switch (index) {
                        case 0 -> dest1;
                        case 1 -> dest2;
                        default -> dest3;
                    };
                    transferService.createTransfer(
                        sourceWalletId,
                        destination.toString(),
                        3000L,
                        "concurrent-" + index
                    );
                    successCount.incrementAndGet();
                } catch (Exception e) {
                    // Expected if insufficient balance
                } finally {
                    latch.countDown();
                }
            });
        }

        // Wait for all transfers to complete
        boolean completed = latch.await(10, TimeUnit.SECONDS);
        //executor.awaitTermination(10, TimeUnit.SECONDS);
        executor.shutdown();
        // Assert
        assertThat(completed).isTrue();

        // Verify final balance (should be debited 3000 * number of successful transfers)
        Wallet finalSource = walletRepository.findById(sourceWalletId).orElseThrow();
        Long expectedBalance = 10000L - (3000L * successCount.get());
        assertThat(finalSource.getBalance()).isEqualTo(expectedBalance);

        // Verify no double-spending (total debits <= initial balance)
        List<LedgerEntry> allDebits = ledgerEntryRepository.findAll()
            .stream()
            .filter(e -> e.getWalletId().equals(sourceWalletId) && e.getEntryType() == LedgerEntry.EntryType.DEBIT)
            .toList();

        Long totalDebits = allDebits.stream()
            .mapToLong(LedgerEntry::getAmount)
            .sum();

        assertThat(totalDebits).isLessThanOrEqualTo(10000L);
    }

    @Test
    @DisplayName("Should maintain ledger balance consistency")
    void testLedgerConsistency() {
        // Arrange
        Long transferAmount = 500L;

        // Act
        for (int i = 0; i < 5; i++) {
            //UUID destinationWallet = UUID.randomUUID();
            Wallet destinationWallet = walletRepository.save(Wallet.builder()
                //.id(destinationWallet.toString())
                .balance(0L)
                .build());

            transferService.createTransfer(
                sourceWalletId,
                destinationWallet.getId(),
                transferAmount,
                "consistency-" + i
            );
        }

        // Assert - verify ledger entries balance
        List<LedgerEntry> allEntries = ledgerEntryRepository.findAll();

        long totalCredits = allEntries.stream()
            .filter(e -> e.getEntryType() == LedgerEntry.EntryType.CREDIT)
            .mapToLong(LedgerEntry::getAmount)
            .sum();

        long totalDebits = allEntries.stream()
            .filter(e -> e.getEntryType() == LedgerEntry.EntryType.DEBIT)
            .mapToLong(LedgerEntry::getAmount)
            .sum();

        assertThat(totalCredits).isEqualTo(totalDebits);
        assertThat(totalDebits).isEqualTo(transferAmount * 5);

        // Verify each transfer has exactly 2 entries
        long transferCount = transferRepository.findAll().size();
        assertThat(allEntries).hasSize((int) (transferCount * 2));
    }

    @Test
    @DisplayName("Should fail gracefully when destination wallet doesn't exist")
    void testTransferToNonExistentWallet() {
        // Arrange
        UUID nonExistentWallet = UUID.randomUUID();

        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                nonExistentWallet.toString(),
                1000L,
                "non-existent-1"
            )
        ).isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Destination wallet not found");
    }

    @Test
    @DisplayName("Should handle rapid sequential transfers atomically")
    void testSequentialTransfersAtomicity() {

        //create wallets
        List<Wallet> walletList = Arrays.asList(
                Wallet.builder().balance(0L).build(),
                Wallet.builder().balance(0L).build()
        );
        walletRepository.saveAll(walletList);

        String dest1 = walletList.get(0).getId();
        String dest2 = walletList.get(1).getId();
        // Act
        Transfer transfer1 = transferService.createTransfer(
            sourceWalletId,
            dest1,
            2000L,
            "seq-1"
        );

        Transfer transfer2 = transferService.createTransfer(
            sourceWalletId,
            dest2,
            3000L,
            "seq-2"
        );

        // Assert
        Wallet finalSource = walletRepository.findById(sourceWalletId).orElseThrow();
        assertThat(finalSource.getBalance()).isEqualTo(10000L - 2000L - 3000L);

        Wallet finalDest1 = walletRepository.findById(dest1).orElseThrow();
        assertThat(finalDest1.getBalance()).isEqualTo(2000L);

        Wallet finalDest2 = walletRepository.findById(dest2).orElseThrow();
        assertThat(finalDest2.getBalance()).isEqualTo(3000L);

        // Verify both transfers are PROCESSED
        assertThat(transfer1.getStatus()).isEqualTo(Transfer.TransferStatus.PROCESSED);
        assertThat(transfer2.getStatus()).isEqualTo(Transfer.TransferStatus.PROCESSED);

        // Verify ledger has 4 entries (2 per transfer)
        assertThat(ledgerEntryRepository.findAll()).hasSize(4);
    }

    @Test
    @DisplayName("Should prevent transfer to self")
    void testTransferToSelf() {
        // Act & Assert
        assertThatThrownBy(() ->
            transferService.createTransfer(
                sourceWalletId,
                sourceWalletId,
                1000L,
                "self-transfer"
            )
        ).isInstanceOf(IllegalArgumentException.class)
            .hasMessageContaining("Source and destination wallets cannot be the same");
    }
}

