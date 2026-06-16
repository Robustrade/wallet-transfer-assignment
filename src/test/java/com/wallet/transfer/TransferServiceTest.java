package com.wallet.transfer;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.wallet.transfer.dto.TransferRequest;
import com.wallet.transfer.dto.TransferResponse;
import com.wallet.transfer.entity.IdempotencyRecord;
import com.wallet.transfer.entity.TransferState;
import com.wallet.transfer.entity.Wallet;
import com.wallet.transfer.repository.LedgerEntryRepository;
import com.wallet.transfer.repository.TransferRepository;
import com.wallet.transfer.repository.WalletRepository;
import com.wallet.transfer.service.IdempotencyService;
import com.wallet.transfer.service.TransferService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InjectMocks;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.math.BigDecimal;
import java.util.Arrays;
import java.util.Optional;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
public class TransferServiceTest {

    @Mock
    private WalletRepository walletRepository;

    @Mock
    private TransferRepository transferRepository;

    @Mock
    private LedgerEntryRepository ledgerEntryRepository;

    @Mock
    private IdempotencyService idempotencyService;

    @Mock
    private ObjectMapper objectMapper;

    @InjectMocks
    private TransferService transferService;

    private TransferRequest request;

    @BeforeEach
    void setUp() {
        request = new TransferRequest();
        request.setIdempotencyKey("key123");
        request.setFromWalletId("w1");
        request.setToWalletId("w2");
        request.setAmount(new BigDecimal("100.00"));
    }

    @Test
    void testExecuteTransfer_InsufficientFunds() {
        // Arrange
        IdempotencyRecord record = new IdempotencyRecord();
        record.setIdempotencyKey("key123");
        when(idempotencyService.checkAndInitializeIdempotencyRecord(request)).thenReturn(record);

        Wallet w1 = new Wallet("w1", new BigDecimal("50.00"), null);
        Wallet w2 = new Wallet("w2", new BigDecimal("100.00"), null);
        
        when(walletRepository.findByIdsForUpdateOrdered(anyList())).thenReturn(Arrays.asList(w1, w2));
        when(transferRepository.save(any())).thenAnswer(i -> i.getArguments()[0]);

        // Act
        TransferResponse response = transferService.executeTransfer(request);

        // Assert
        assertEquals(TransferState.FAILED, response.getState());
        assertEquals("Insufficient funds", response.getMessage());
        verify(idempotencyService).updateIdempotencyRecord(eq("key123"), any(), eq(400));
        verify(ledgerEntryRepository, never()).saveAll(any());
    }

    @Test
    void testExecuteTransfer_Success() {
        // Arrange
        IdempotencyRecord record = new IdempotencyRecord();
        record.setIdempotencyKey("key123");
        when(idempotencyService.checkAndInitializeIdempotencyRecord(request)).thenReturn(record);

        Wallet w1 = new Wallet("w1", new BigDecimal("150.00"), null);
        Wallet w2 = new Wallet("w2", new BigDecimal("100.00"), null);
        
        when(walletRepository.findByIdsForUpdateOrdered(anyList())).thenReturn(Arrays.asList(w1, w2));
        when(transferRepository.save(any())).thenAnswer(i -> {
            com.wallet.transfer.entity.Transfer t = (com.wallet.transfer.entity.Transfer) i.getArguments()[0];
            t.setId("t1");
            return t;
        });

        // Act
        TransferResponse response = transferService.executeTransfer(request);

        // Assert
        assertEquals(TransferState.PROCESSED, response.getState());
        assertEquals(new BigDecimal("50.00"), w1.getBalance());
        assertEquals(new BigDecimal("200.00"), w2.getBalance());
        verify(ledgerEntryRepository).saveAll(any());
        verify(idempotencyService).updateIdempotencyRecord(eq("key123"), any(), eq(201));
    }
}
