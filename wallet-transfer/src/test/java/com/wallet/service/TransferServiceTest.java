package com.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.wallet.domain.Transfer;
import com.wallet.domain.TransferStatus;
import com.wallet.dto.TransferExecutionResult;
import com.wallet.dto.TransferRequest;
import com.wallet.dto.TransferResponse;
import com.wallet.exception.InsufficientFundsException;
import com.wallet.exception.SelfTransferException;
import com.wallet.exception.TransferNotFoundException;
import com.wallet.repository.TransferRepository;
import com.wallet.service.TransferOrchestrator.IdempotencyOutcome;
import java.math.BigDecimal;
import java.time.Instant;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

@ExtendWith(MockitoExtension.class)
class TransferServiceTest {

    @Mock
    private TransferOrchestrator orchestrator;

    @Mock
    private TransferRepository transferRepository;

    @InjectMocks
    private TransferServiceImpl transferService;

    @Test
    void executeTransfer_selfTransfer_throwsBeforeAnyOrchestratorCall() {
        TransferRequest request = new TransferRequest("key-1", "wallet_a", "wallet_a", new BigDecimal("100.00"));

        assertThatThrownBy(() -> transferService.executeTransfer(request))
            .isInstanceOf(SelfTransferException.class);

        verify(orchestrator, never()).getOrCreatePendingTransfer(any());
    }

    @Test
    void executeTransfer_idempotentReplay_returnsExistingWithoutProcessing() {
        Transfer existing = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));
        existing.markProcessed();

        when(orchestrator.getOrCreatePendingTransfer(any()))
            .thenReturn(new IdempotencyOutcome(existing, true));

        TransferRequest request = new TransferRequest("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));
        TransferExecutionResult result = transferService.executeTransfer(request);

        assertThat(result.isNewTransfer()).isFalse();
        assertThat(result.transfer().status()).isEqualTo(TransferStatus.PROCESSED);
        verify(orchestrator, never()).processTransfer(any());
    }

    @Test
    void executeTransfer_newTransfer_delegatesToOrchestratorPhase2() {
        Transfer pending = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));
        TransferResponse processedResponse = new TransferResponse(
            UUID.randomUUID(), "key-1", "wallet_a", "wallet_b",
            new BigDecimal("100.00"), TransferStatus.PROCESSED, null, Instant.now()
        );

        when(orchestrator.getOrCreatePendingTransfer(any()))
            .thenReturn(new IdempotencyOutcome(pending, false));
        when(orchestrator.processTransfer(pending)).thenReturn(processedResponse);

        TransferRequest request = new TransferRequest("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));
        TransferExecutionResult result = transferService.executeTransfer(request);

        assertThat(result.isNewTransfer()).isTrue();
        assertThat(result.transfer().status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void executeTransfer_insufficientFunds_marksFailedAndRethrows() {
        Transfer pending = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("9999.00"));

        when(orchestrator.getOrCreatePendingTransfer(any()))
            .thenReturn(new IdempotencyOutcome(pending, false));
        when(orchestrator.processTransfer(pending))
            .thenThrow(new InsufficientFundsException("Wallet wallet_a has insufficient balance"));

        TransferRequest request = new TransferRequest("key-1", "wallet_a", "wallet_b", new BigDecimal("9999.00"));

        assertThatThrownBy(() -> transferService.executeTransfer(request))
            .isInstanceOf(InsufficientFundsException.class);

        verify(orchestrator).markTransferFailed(pending.getId(), "Wallet wallet_a has insufficient balance");
    }

    @Test
    void executeTransfer_walletNotFound_marksFailedAndRethrows() {
        Transfer pending = Transfer.pending("key-1", "wallet_a", "ghost", new BigDecimal("50.00"));

        when(orchestrator.getOrCreatePendingTransfer(any()))
            .thenReturn(new IdempotencyOutcome(pending, false));
        when(orchestrator.processTransfer(pending))
            .thenThrow(new com.wallet.exception.WalletNotFoundException("ghost"));

        TransferRequest request = new TransferRequest("key-1", "wallet_a", "ghost", new BigDecimal("50.00"));

        assertThatThrownBy(() -> transferService.executeTransfer(request))
            .isInstanceOf(com.wallet.exception.WalletNotFoundException.class);

        verify(orchestrator).markTransferFailed(any(), any());
    }

    @Test
    void getTransfer_found_returnsResponse() {
        UUID id = UUID.randomUUID();
        Transfer transfer = Transfer.pending("key-1", "wallet_a", "wallet_b", new BigDecimal("100.00"));
        transfer.markProcessed();

        when(transferRepository.findById(id)).thenReturn(Optional.of(transfer));

        TransferResponse response = transferService.getTransfer(id);

        assertThat(response.status()).isEqualTo(TransferStatus.PROCESSED);
    }

    @Test
    void getTransfer_notFound_throwsTransferNotFoundException() {
        UUID id = UUID.randomUUID();
        when(transferRepository.findById(id)).thenReturn(Optional.empty());

        assertThatThrownBy(() -> transferService.getTransfer(id))
            .isInstanceOf(TransferNotFoundException.class);
    }
}
