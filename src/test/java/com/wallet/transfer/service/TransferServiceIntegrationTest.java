package com.wallet.transfer.service;

import com.wallet.transfer.BaseIntegrationTest;
import com.wallet.transfer.domain.LedgerEntry;
import com.wallet.transfer.domain.LedgerEntryType;
import com.wallet.transfer.domain.Transfer;
import com.wallet.transfer.domain.TransferStatus;
import com.wallet.transfer.domain.Wallet;
import com.wallet.transfer.dto.TransferResult;
import com.wallet.transfer.exception.InsufficientFundsException;
import com.wallet.transfer.exception.InvalidTransferRequestException;
import com.wallet.transfer.exception.WalletNotFoundException;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.jdbc.core.JdbcTemplate;

import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

class TransferServiceIntegrationTest extends BaseIntegrationTest {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @Autowired
    private JdbcTemplate jdbcTemplate;

    @BeforeEach
    void setUp() {
        jdbcTemplate.execute("TRUNCATE TABLE ledger_entries, transfers, wallets CASCADE");

        walletRepository.save(new Wallet("wallet_1", 1000L));
        walletRepository.save(new Wallet("wallet_2", 500L));
        walletRepository.save(new Wallet("wallet_3", 0L));
    }

    @Nested
    @DisplayName("Transfer Execution")
    class TransferExecution {

        @Test
        @DisplayName("should execute transfer successfully and update balances")
        void successfulTransfer() {
            TransferResult result = transferService.executeTransfer(
                    "key-1", "wallet_1", "wallet_2", 200L);

            assertThat(result.created()).isTrue();
            assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
            assertThat(result.transfer().getFromWalletId()).isEqualTo("wallet_1");
            assertThat(result.transfer().getToWalletId()).isEqualTo("wallet_2");
            assertThat(result.transfer().getAmount()).isEqualTo(200L);

            Wallet source = walletRepository.findById("wallet_1").orElseThrow();
            Wallet dest = walletRepository.findById("wallet_2").orElseThrow();

            assertThat(source.getBalance()).isEqualTo(800L);
            assertThat(dest.getBalance()).isEqualTo(700L);
        }

        @Test
        @DisplayName("should fail transfer when source has insufficient funds")
        void insufficientFunds() {
            assertThatThrownBy(() ->
                    transferService.executeTransfer("key-2", "wallet_1", "wallet_2", 1500L))
                    .isInstanceOf(InsufficientFundsException.class);

            // Balances unchanged
            Wallet source = walletRepository.findById("wallet_1").orElseThrow();
            assertThat(source.getBalance()).isEqualTo(1000L);

            // Transfer marked as FAILED
            Transfer transfer = transferRepository.findByIdempotencyKey("key-2").orElseThrow();
            assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
        }

        @Test
        @DisplayName("should throw when source wallet does not exist")
        void sourceWalletNotFound() {
            assertThatThrownBy(() ->
                    transferService.executeTransfer("key-3", "nonexistent", "wallet_2", 100L))
                    .isInstanceOf(WalletNotFoundException.class);
        }

        @Test
        @DisplayName("should throw when destination wallet does not exist")
        void destWalletNotFound() {
            assertThatThrownBy(() ->
                    transferService.executeTransfer("key-4", "wallet_1", "nonexistent", 100L))
                    .isInstanceOf(WalletNotFoundException.class);
        }

        @Test
        @DisplayName("should throw when source and destination are the same wallet")
        void sameWallet() {
            assertThatThrownBy(() ->
                    transferService.executeTransfer("key-5", "wallet_1", "wallet_1", 100L))
                    .isInstanceOf(InvalidTransferRequestException.class)
                    .hasMessageContaining("different");
        }

        @Test
        @DisplayName("should succeed when transfer amount exactly equals balance (drains to zero)")
        void exactBalanceTransfer() {
            TransferResult result = transferService.executeTransfer(
                    "key-exact", "wallet_1", "wallet_2", 1000L);

            assertThat(result.created()).isTrue();
            assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);

            Wallet source = walletRepository.findById("wallet_1").orElseThrow();
            Wallet dest = walletRepository.findById("wallet_2").orElseThrow();

            assertThat(source.getBalance()).isEqualTo(0L);
            assertThat(dest.getBalance()).isEqualTo(1500L);
        }
    }

    @Nested
    @DisplayName("Idempotency")
    class Idempotency {

        @Test
        @DisplayName("should return same result for duplicate idempotency key")
        void duplicateRequest() {
            TransferResult first = transferService.executeTransfer(
                    "idem-1", "wallet_1", "wallet_2", 100L);
            TransferResult second = transferService.executeTransfer(
                    "idem-1", "wallet_1", "wallet_2", 100L);

            assertThat(first.transfer().getId()).isEqualTo(second.transfer().getId());
            assertThat(first.transfer().getStatus()).isEqualTo(second.transfer().getStatus());
            assertThat(first.created()).isTrue();
            assertThat(second.created()).isFalse();

            // Balance only debited once
            Wallet source = walletRepository.findById("wallet_1").orElseThrow();
            assertThat(source.getBalance()).isEqualTo(900L);
        }

        @Test
        @DisplayName("should not create duplicate ledger entries on retry")
        void noDuplicateLedgerEntries() {
            TransferResult first = transferService.executeTransfer(
                    "idem-2", "wallet_1", "wallet_2", 50L);
            transferService.executeTransfer(
                    "idem-2", "wallet_1", "wallet_2", 50L);

            List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(first.transfer().getId());
            assertThat(entries).hasSize(2);
        }

        @Test
        @DisplayName("should reject when same idempotency key is used with different payload")
        void payloadMismatch() {
            transferService.executeTransfer("idem-conflict", "wallet_1", "wallet_2", 100L);

            // Same key, different amount
            assertThatThrownBy(() ->
                    transferService.executeTransfer("idem-conflict", "wallet_1", "wallet_2", 200L))
                    .isInstanceOf(com.wallet.transfer.exception.IdempotencyKeyConflictException.class);

            // Same key, different destination
            assertThatThrownBy(() ->
                    transferService.executeTransfer("idem-conflict", "wallet_1", "wallet_3", 100L))
                    .isInstanceOf(com.wallet.transfer.exception.IdempotencyKeyConflictException.class);
        }

        @Test
        @DisplayName("should re-throw InsufficientFundsException on replay of FAILED transfer")
        void failedTransferReplay() {
            // First attempt fails
            assertThatThrownBy(() ->
                    transferService.executeTransfer("idem-fail", "wallet_3", "wallet_1", 100L))
                    .isInstanceOf(InsufficientFundsException.class);

            // Retry with same idempotency key should also fail (not return 201 with FAILED status)
            assertThatThrownBy(() ->
                    transferService.executeTransfer("idem-fail", "wallet_3", "wallet_1", 100L))
                    .isInstanceOf(InsufficientFundsException.class);

            // Only one transfer record exists
            long count = transferRepository.count();
            assertThat(count).isEqualTo(1L);
        }
    }

    @Nested
    @DisplayName("Ledger Correctness")
    class LedgerCorrectness {

        @Test
        @DisplayName("should create exactly two ledger entries per transfer")
        void doubleEntry() {
            TransferResult result = transferService.executeTransfer(
                    "ledger-1", "wallet_1", "wallet_2", 300L);
            Transfer transfer = result.transfer();

            List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(transfer.getId());
            assertThat(entries).hasSize(2);

            LedgerEntry debit = entries.stream()
                    .filter(e -> e.getEntryType() == LedgerEntryType.DEBIT)
                    .findFirst().orElseThrow();
            LedgerEntry credit = entries.stream()
                    .filter(e -> e.getEntryType() == LedgerEntryType.CREDIT)
                    .findFirst().orElseThrow();

            assertThat(debit.getWalletId()).isEqualTo("wallet_1");
            assertThat(debit.getAmount()).isEqualTo(300L);
            assertThat(credit.getWalletId()).isEqualTo("wallet_2");
            assertThat(credit.getAmount()).isEqualTo(300L);
        }

        @Test
        @DisplayName("ledger should always balance (total debits == total credits)")
        void ledgerBalances() {
            transferService.executeTransfer("bal-1", "wallet_1", "wallet_2", 100L);
            transferService.executeTransfer("bal-2", "wallet_2", "wallet_1", 50L);
            transferService.executeTransfer("bal-3", "wallet_1", "wallet_3", 200L);

            List<LedgerEntry> allEntries = ledgerEntryRepository.findAll();

            long totalDebits = allEntries.stream()
                    .filter(e -> e.getEntryType() == LedgerEntryType.DEBIT)
                    .mapToLong(LedgerEntry::getAmount)
                    .sum();
            long totalCredits = allEntries.stream()
                    .filter(e -> e.getEntryType() == LedgerEntryType.CREDIT)
                    .mapToLong(LedgerEntry::getAmount)
                    .sum();

            assertThat(totalDebits).isEqualTo(totalCredits);
        }
    }

    @Nested
    @DisplayName("State Transitions")
    class StateTransitions {

        @Test
        @DisplayName("successful transfer should be in PROCESSED state")
        void processedState() {
            TransferResult result = transferService.executeTransfer(
                    "state-1", "wallet_1", "wallet_2", 100L);
            assertThat(result.transfer().getStatus()).isEqualTo(TransferStatus.PROCESSED);
        }

        @Test
        @DisplayName("failed transfer should be in FAILED state")
        void failedState() {
            try {
                transferService.executeTransfer("state-2", "wallet_3", "wallet_1", 100L);
            } catch (InsufficientFundsException ignored) {
            }

            Transfer transfer = transferRepository.findByIdempotencyKey("state-2").orElseThrow();
            assertThat(transfer.getStatus()).isEqualTo(TransferStatus.FAILED);
        }
    }
}
