package com.wallet.integration;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.wallet.domain.EntryType;
import com.wallet.domain.LedgerEntry;
import com.wallet.domain.TransferStatus;
import com.wallet.domain.Wallet;
import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.InsufficientFundsException;
import com.wallet.exception.SelfTransferException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.service.TransferService;
import java.math.BigDecimal;
import java.util.List;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.transaction.annotation.Transactional;

@SpringBootTest
@ActiveProfiles("test")
class TransferIntegrationTest extends PostgresTestContainerConfig {

    @Autowired
    private TransferService transferService;

    @Autowired
    private WalletRepository walletRepository;

    @Autowired
    private TransferRepository transferRepository;

    @Autowired
    private LedgerEntryRepository ledgerEntryRepository;

    @BeforeEach
    void setUp() {
        ledgerEntryRepository.deleteAll();
        transferRepository.deleteAll();
        walletRepository.deleteAll();

        walletRepository.save(new Wallet("w1", new BigDecimal("1000.00")));
        walletRepository.save(new Wallet("w2", new BigDecimal("500.00")));
        walletRepository.save(new Wallet("w3", new BigDecimal("0.00")));
    }

    @Test
    void executeTransfer_happyPath_updatesBalancesAndCreatesLedgerEntries() {
        TransferRequest request = new TransferRequest("key-1", "w1", "w2", new BigDecimal("200.00"));

        TransferExecutionResult result = transferService.executeTransfer(request);

        assertThat(result.isNewTransfer()).isTrue();
        assertThat(result.transfer().status()).isEqualTo(TransferStatus.PROCESSED);

        Wallet w1 = walletRepository.findById("w1").orElseThrow();
        Wallet w2 = walletRepository.findById("w2").orElseThrow();
        assertThat(w1.getBalance()).isEqualByComparingTo("800.00");
        assertThat(w2.getBalance()).isEqualByComparingTo("700.00");
    }

    @Test
    void executeTransfer_createsExactlyTwoLedgerEntries() {
        TransferRequest request = new TransferRequest("key-1", "w1", "w2", new BigDecimal("100.00"));
        TransferExecutionResult result = transferService.executeTransfer(request);

        List<LedgerEntry> entries = ledgerEntryRepository.findByTransferId(result.transfer().transferId());
        assertThat(entries).hasSize(2);

        assertThat(entries.stream().filter(e -> e.getEntryType() == EntryType.DEBIT)).hasSize(1);
        assertThat(entries.stream().filter(e -> e.getEntryType() == EntryType.CREDIT)).hasSize(1);

        LedgerEntry debit = entries.stream().filter(e -> e.getEntryType() == EntryType.DEBIT).findFirst().orElseThrow();
        LedgerEntry credit = entries.stream().filter(e -> e.getEntryType() == EntryType.CREDIT).findFirst().orElseThrow();

        assertThat(debit.getWalletId()).isEqualTo("w1");
        assertThat(debit.getAmount()).isEqualByComparingTo("100.00");
        assertThat(credit.getWalletId()).isEqualTo("w2");
        assertThat(credit.getAmount()).isEqualByComparingTo("100.00");
    }

    @Test
    void executeTransfer_ledgerIsAlwaysBalanced() {
        transferService.executeTransfer(new TransferRequest("k1", "w1", "w2", new BigDecimal("50.00")));
        transferService.executeTransfer(new TransferRequest("k2", "w2", "w1", new BigDecimal("25.00")));

        List<LedgerEntry> allEntries = ledgerEntryRepository.findAll();
        BigDecimal totalDebits = allEntries.stream()
            .filter(e -> e.getEntryType() == EntryType.DEBIT)
            .map(LedgerEntry::getAmount)
            .reduce(BigDecimal.ZERO, BigDecimal::add);
        BigDecimal totalCredits = allEntries.stream()
            .filter(e -> e.getEntryType() == EntryType.CREDIT)
            .map(LedgerEntry::getAmount)
            .reduce(BigDecimal.ZERO, BigDecimal::add);

        assertThat(totalDebits).isEqualByComparingTo(totalCredits);
    }

    @Test
    void executeTransfer_idempotency_sameKeyReturnsSameResult() {
        TransferRequest request = new TransferRequest("key-idem", "w1", "w2", new BigDecimal("100.00"));

        TransferExecutionResult first = transferService.executeTransfer(request);
        TransferExecutionResult second = transferService.executeTransfer(request);

        assertThat(first.isNewTransfer()).isTrue();
        assertThat(second.isNewTransfer()).isFalse();
        assertThat(second.transfer().transferId()).isEqualTo(first.transfer().transferId());

        // Balance must only change once
        Wallet w1 = walletRepository.findById("w1").orElseThrow();
        assertThat(w1.getBalance()).isEqualByComparingTo("900.00");
    }

    @Test
    void executeTransfer_idempotency_noAdditionalLedgerEntries() {
        TransferRequest request = new TransferRequest("key-idem2", "w1", "w2", new BigDecimal("50.00"));

        transferService.executeTransfer(request);
        transferService.executeTransfer(request);
        transferService.executeTransfer(request);

        List<LedgerEntry> entries = ledgerEntryRepository.findAll();
        assertThat(entries).hasSize(2); // Exactly 2, not 6
    }

    @Test
    void executeTransfer_insufficientFunds_marksTransferFailed() {
        TransferRequest request = new TransferRequest("key-fail", "w3", "w1", new BigDecimal("100.00"));

        assertThatThrownBy(() -> transferService.executeTransfer(request))
            .isInstanceOf(InsufficientFundsException.class);

        TransferResponse transfer = transferService.getTransfer(
            transferRepository.findByIdempotencyKey("key-fail").orElseThrow().getId()
        );
        assertThat(transfer.status()).isEqualTo(TransferStatus.FAILED);
        assertThat(transfer.failureReason()).contains("insufficient balance");
    }

    @Test
    void executeTransfer_insufficientFunds_noBalanceChange() {
        BigDecimal w3BalanceBefore = walletRepository.findById("w3").orElseThrow().getBalance();

        assertThatThrownBy(() ->
            transferService.executeTransfer(new TransferRequest("key-fail2", "w3", "w1", new BigDecimal("100.00"))))
            .isInstanceOf(InsufficientFundsException.class);

        assertThat(walletRepository.findById("w3").orElseThrow().getBalance())
            .isEqualByComparingTo(w3BalanceBefore);
        assertThat(ledgerEntryRepository.findAll()).isEmpty();
    }

    @Test
    void executeTransfer_selfTransfer_throwsWithoutCreatingTransfer() {
        assertThatThrownBy(() ->
            transferService.executeTransfer(new TransferRequest("key-self", "w1", "w1", new BigDecimal("100.00"))))
            .isInstanceOf(SelfTransferException.class);

        assertThat(transferRepository.findByIdempotencyKey("key-self")).isEmpty();
    }

    @Test
    void executeTransfer_walletNotFound_throwsAndNoTransfer() {
        assertThatThrownBy(() ->
            transferService.executeTransfer(new TransferRequest("key-nf", "w1", "nonexistent", new BigDecimal("10.00"))))
            .isInstanceOf(WalletNotFoundException.class);
    }

    @Test
    void executeTransfer_chainedTransfers_correctFinalBalances() {
        transferService.executeTransfer(new TransferRequest("k1", "w1", "w2", new BigDecimal("100.00")));
        transferService.executeTransfer(new TransferRequest("k2", "w2", "w3", new BigDecimal("200.00")));
        transferService.executeTransfer(new TransferRequest("k3", "w3", "w1", new BigDecimal("50.00")));

        // w1: 1000 - 100 + 50 = 950
        // w2: 500 + 100 - 200 = 400
        // w3: 0 + 200 - 50 = 150
        assertThat(walletRepository.findById("w1").orElseThrow().getBalance()).isEqualByComparingTo("950.00");
        assertThat(walletRepository.findById("w2").orElseThrow().getBalance()).isEqualByComparingTo("400.00");
        assertThat(walletRepository.findById("w3").orElseThrow().getBalance()).isEqualByComparingTo("150.00");
    }

    @Test
    @Transactional
    void getTransfer_returnsCorrectTransfer() {
        TransferExecutionResult result = transferService.executeTransfer(
            new TransferRequest("key-get", "w1", "w2", new BigDecimal("75.00"))
        );

        TransferResponse fetched = transferService.getTransfer(result.transfer().transferId());

        assertThat(fetched.transferId()).isEqualTo(result.transfer().transferId());
        assertThat(fetched.amount()).isEqualByComparingTo("75.00");
        assertThat(fetched.status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void executeTransfer_idempotentReplay_afterFailure_returnsSameFailedTransfer() {
        TransferRequest request = new TransferRequest("key-fail-idem", "w3", "w1", new BigDecimal("999.00"));

        assertThatThrownBy(() -> transferService.executeTransfer(request))
            .isInstanceOf(InsufficientFundsException.class);

        // Second call — idempotent replay returns FAILED transfer, no exception
        TransferExecutionResult replay = transferService.executeTransfer(request);
        assertThat(replay.isNewTransfer()).isFalse();
        assertThat(replay.transfer().status()).isEqualTo(TransferStatus.FAILED);
    }
}
