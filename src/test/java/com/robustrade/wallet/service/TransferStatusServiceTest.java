package com.robustrade.wallet.service;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.robustrade.wallet.domain.Transfer;
import com.robustrade.wallet.domain.TransferState;
import com.robustrade.wallet.repository.TransferRepository;
import java.math.BigDecimal;
import java.util.Optional;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

@ExtendWith(MockitoExtension.class)
class TransferStatusServiceTest {

    @Mock
    private TransferRepository transferRepository;

    private TransferStatusService transferStatusService;

    @BeforeEach
    void setUp() {
        transferStatusService = new TransferStatusService(transferRepository);
    }

    @Test
    void markFailed_whenPending_locksRowAndMarksFailed() {
        Transfer transfer = Transfer.createPending("key-1", "wallet-a", "wallet-b", new BigDecimal("10.00"));
        when(transferRepository.findByIdForUpdate(transfer.getId())).thenReturn(Optional.of(transfer));
        when(transferRepository.save(any(Transfer.class))).thenAnswer(inv -> inv.getArgument(0));

        transferStatusService.markFailed(transfer.getId(), "Insufficient balance");

        verify(transferRepository).findByIdForUpdate(transfer.getId());
        verify(transferRepository, never()).findById(any(UUID.class));
        ArgumentCaptor<Transfer> captor = ArgumentCaptor.forClass(Transfer.class);
        verify(transferRepository).save(captor.capture());
        assertThat(captor.getValue().getState()).isEqualTo(TransferState.FAILED);
        assertThat(captor.getValue().getFailureReason()).isEqualTo("Insufficient balance");
    }

    @Test
    void markFailed_whenAlreadyProcessed_doesNotOverwrite() {
        Transfer transfer = Transfer.createPending("key-1", "wallet-a", "wallet-b", new BigDecimal("10.00"));
        transfer.markProcessed();
        when(transferRepository.findByIdForUpdate(transfer.getId())).thenReturn(Optional.of(transfer));

        transferStatusService.markFailed(transfer.getId(), "late failure after success");

        verify(transferRepository).findByIdForUpdate(transfer.getId());
        verify(transferRepository, never()).save(any());
        assertThat(transfer.getState()).isEqualTo(TransferState.PROCESSED);
        assertThat(transfer.getFailureReason()).isNull();
    }
}
