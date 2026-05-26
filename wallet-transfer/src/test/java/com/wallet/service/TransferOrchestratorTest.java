package com.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.wallet.domain.Transfer;
import com.wallet.domain.Wallet;
import com.wallet.exception.InsufficientFundsException;
import com.wallet.exception.WalletNotFoundException;
import com.wallet.repository.LedgerEntryRepository;
import com.wallet.repository.TransferRepository;
import com.wallet.repository.WalletRepository;
import com.wallet.service.TransferOrchestrator.IdempotencyOutcome;
import java.math.BigDecimal;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.dao.DataIntegrityViolationException;

@ExtendWith(MockitoExtension.class)
class TransferOrchestratorTest {

    @Mock
    private TransferRepository transferRepository;

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @InjectMocks
    private TransferOrchestrator orchestrator;

    private Wallet fromWallet;
    private Wallet toWallet;

    @BeforeEach
    void setUp() {
        fromWallet = new Wallet("wallet_a", new BigDecimal("500.00"));
        toWallet = new Wallet("wallet_b", new BigDecimal("100.00"));
    }

    @Test
    void getOrCreatePendingTransfer_existingKey_returnsDuplicate() {
        Transfer existing = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("50.00"));
        when(transferRepository.findByIdempotencyKey("key-1")).thenReturn(Optional.of(existing));

        IdempotencyOutcome outcome = orchestrator.getOrCreatePendingTransfer(
            new com.wallet.dto.TransferRequest("key-1", "wallet_a", "wallet_b", new BigDecimal("50.00"))
        );

        assertThat(outcome.isDuplicate()).isTrue();
        assertThat(outcome.transfer().getIdempotencyKey()).isEqualTo("key-1");
        verify(transferRepository, never()).saveAndFlush(any());
    }

    @Test
    void getOrCreatePendingTransfer_newKey_savesAndReturnsFresh() {
        when(transferRepository.findByIdempotencyKey("key-new")).thenReturn(Optional.empty());
        Transfer saved = Transfer.pending("key-new", "wallet_a", "wallet_b", new BigDecimal("75.00"));
        when(transferRepository.saveAndFlush(any())).thenReturn(saved);

        IdempotencyOutcome outcome = orchestrator.getOrCreatePendingTransfer(
            new com.wallet.dto.TransferRequest("key-new", "wallet_a", "wallet_b", new BigDecimal("75.00"))
        );

        assertThat(outcome.isDuplicate()).isFalse();
        verify(transferRepository).saveAndFlush(any());
    }

    @Test
    void getOrCreatePendingTransfer_concurrentDuplicate_catchesConstraintAndReturnsExisting() {
        Transfer existing = Transfer.pending("key-race", "wallet_a", "wallet_b", new BigDecimal("50.00"));
        when(transferRepository.findByIdempotencyKey("key-race"))
            .thenReturn(Optional.empty())
            .thenReturn(Optional.of(existing));
        when(transferRepository.saveAndFlush(any())).thenThrow(new DataIntegrityViolationException("unique constraint"));

        IdempotencyOutcome outcome = orchestrator.getOrCreatePendingTransfer(
            new com.wallet.dto.TransferRequest("key-race", "wallet_a", "wallet_b", new BigDecimal("50.00"))
        );

        assertThat(outcome.isDuplicate()).isTrue();
        assertThat(outcome.transfer().getIdempotencyKey()).isEqualTo("key-race");
    }

    @Test
    void processTransfer_insufficientFunds_throwsWithoutSavingLedger() {
        Wallet poorWallet = new Wallet("wallet_a", new BigDecimal("10.00"));
        Transfer pending = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));

        when(walletRepository.findByIdForUpdate("wallet_a")).thenReturn(Optional.of(poorWallet));
        when(walletRepository.findByIdForUpdate("wallet_b")).thenReturn(Optional.of(toWallet));

        assertThatThrownBy(() -> orchestrator.processTransfer(pending))
            .isInstanceOf(InsufficientFundsException.class);

        verify(walletRepository, never()).save(any());
        verify(ledgerEntryRepository, never()).save(any());
    }

    @Test
    void processTransfer_walletNotFound_throws() {
        Transfer pending = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("50.00"));

        when(walletRepository.findByIdForUpdate("wallet_a")).thenReturn(Optional.empty());

        assertThatThrownBy(() -> orchestrator.processTransfer(pending))
            .isInstanceOf(WalletNotFoundException.class);
    }

    @Test
    void processTransfer_locksWalletsInAscendingIdOrder() {
        // wallet_b debits (from), wallet_a credits (to)
        // but lock order must be wallet_a first (ascending)
        Transfer pending = Transfer.pending("key-1", "wallet_b", "wallet_a", new BigDecimal("50.00"));

        when(walletRepository.findByIdForUpdate("wallet_a")).thenReturn(Optional.of(fromWallet));
        when(walletRepository.findByIdForUpdate("wallet_b")).thenReturn(Optional.of(toWallet));
        when(transferRepository.findById(any())).thenReturn(Optional.of(pending));
        when(walletRepository.save(any())).thenAnswer(inv -> inv.getArgument(0));
        when(ledgerEntryRepository.save(any())).thenAnswer(inv -> inv.getArgument(0));
        when(transferRepository.save(any())).thenAnswer(inv -> inv.getArgument(0));

        orchestrator.processTransfer(pending);

        var inOrder = org.mockito.Mockito.inOrder(walletRepository);
        inOrder.verify(walletRepository).findByIdForUpdate("wallet_a");
        inOrder.verify(walletRepository).findByIdForUpdate("wallet_b");
    }

    @Test
    void markTransferFailed_pendingTransfer_marksAsFailed() {
        UUID id = UUID.randomUUID();
        Transfer pending = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("50.00"));
        when(transferRepository.findById(id)).thenReturn(Optional.of(pending));
        when(transferRepository.save(any())).thenAnswer(inv -> inv.getArgument(0));

        orchestrator.markTransferFailed(id, "Insufficient funds");

        assertThat(pending.getFailureReason()).isEqualTo("Insufficient funds");
        verify(transferRepository).save(pending);
    }

    @Test
    void markTransferFailed_alreadyProcessed_doesNotOverwrite() {
        UUID id = UUID.randomUUID();
        Transfer processed = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("50.00"));
        processed.markProcessed();
        when(transferRepository.findById(id)).thenReturn(Optional.of(processed));

        orchestrator.markTransferFailed(id, "late error");

        verify(transferRepository, never()).save(any());
    }
}
